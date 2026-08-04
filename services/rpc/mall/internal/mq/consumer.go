package mq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"

	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_event_inbox"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
)

const consumerGroupName = "mall-order-consumer"

// OrderEventConsumer 订单事件消费者，监听 RocketMQ 订单相关 Topic
type OrderEventConsumer struct {
	cfg      rocketmq.Config
	skuStore product_skus.ProductSkusModel
	redis    redis.UniversalClient
	inbox    mall_order_event_inbox.MallOrderEventInboxModel
	consumer rocketmq.Consumer
	quit     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
}

// NewOrderEventConsumer 创建订单事件消费者
func NewOrderEventConsumer(cfg rocketmq.Config, skuStore product_skus.ProductSkusModel, rds redis.UniversalClient, inbox mall_order_event_inbox.MallOrderEventInboxModel) *OrderEventConsumer {
	return &OrderEventConsumer{
		cfg:      cfg,
		skuStore: skuStore,
		redis:    rds,
		inbox:    inbox,
		quit:     make(chan struct{}),
	}
}

func (c *OrderEventConsumer) Start() {
	if len(c.cfg.NameServers) == 0 {
		logx.Info("rocketmq not configured, order event consumer not started")
		return
	}

	for {
		if c.stopping() {
			return
		}
		consumer, err := c.newConsumer()
		if err != nil {
			logx.Errorf("start rocketmq order event consumer failed, retrying: %v", err)
			if !c.waitRetry(5 * time.Second) {
				return
			}
			continue
		}
		if !c.setConsumer(consumer) {
			_ = consumer.Shutdown()
			return
		}
		logx.Info("order event consumer started")
		<-c.quit
		return
	}
}

func (c *OrderEventConsumer) Stop() {
	c.stopOnce.Do(func() {
		close(c.quit)
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.consumer != nil {
			_ = c.consumer.Shutdown()
			c.consumer = nil
		}
		logx.Info("order event consumer stopped")
	})
}

func (c *OrderEventConsumer) newConsumer() (rocketmq.Consumer, error) {
	cfg := c.cfg
	cfg.GroupName = consumerGroupName
	consumer, err := rocketmq.NewConsumer(cfg)
	if err != nil {
		return nil, err
	}
	for _, topic := range []string{TopicOrderCreated, TopicOrderPaid, TopicOrderCancelled} {
		currentTopic := topic
		if err := consumer.Subscribe(currentTopic, "", func(ctx context.Context, body []byte) error {
			return c.handleMessage(ctx, currentTopic, body)
		}); err != nil {
			_ = consumer.Shutdown()
			return nil, err
		}
	}
	if err := consumer.Start(); err != nil {
		_ = consumer.Shutdown()
		return nil, err
	}
	return consumer, nil
}

func (c *OrderEventConsumer) setConsumer(consumer rocketmq.Consumer) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.quit:
		return false
	default:
	}
	c.consumer = consumer
	return true
}

func (c *OrderEventConsumer) stopping() bool {
	select {
	case <-c.quit:
		return true
	default:
		return false
	}
}

func (c *OrderEventConsumer) waitRetry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.quit:
		return false
	}
}

func (c *OrderEventConsumer) handleMessage(ctx context.Context, topic string, body []byte) error {
	event, err := DecodeOrderEvent(body)
	if err != nil {
		logx.Errorf("decode order event failed: %v, topic=%s, body=%s", err, topic, string(body))
		// 返回 nil 避免 poison message 无限重试
		return nil
	}
	dedupKey := NormalizeOrderEventDedupKey(event)
	if dedupKey == "" {
		return fmt.Errorf("order event dedup key is empty: topic=%s order_id=%s event_type=%s", topic, event.OrderID, event.EventType)
	}
	if c.inbox == nil {
		return fmt.Errorf("order event inbox is nil")
	}

	now := time.Now()
	claim, err := c.inbox.Claim(ctx, consumerGroupName, dedupKey, event.EventType, now, now.Add(5*time.Minute))
	if err != nil {
		return fmt.Errorf("claim order event inbox: %w", err)
	}
	switch claim {
	case mall_order_event_inbox.AlreadyProcessed:
		logx.Infof("skip duplicate order event: topic=%s dedup_key=%s", topic, dedupKey)
		return nil
	case mall_order_event_inbox.AlreadyProcessing:
		return fmt.Errorf("order event is already processing: dedup_key=%s", dedupKey)
	}

	logx.Infof("handle order event: topic=%s, order_id=%s, user_id=%s, sku_id=%s, qty=%d",
		topic, event.OrderID, event.UserID, event.SkuID, event.Quantity)

	if err := c.processEvent(ctx, topic, event); err != nil {
		_, markErr := c.inbox.MarkRetry(ctx, consumerGroupName, dedupKey, err.Error(), time.Now())
		if markErr != nil {
			return fmt.Errorf("process order event: %v; mark inbox retry: %w", err, markErr)
		}
		return err
	}
	ok, err := c.inbox.MarkDone(ctx, consumerGroupName, dedupKey, time.Now())
	if err != nil {
		return fmt.Errorf("mark order event inbox done: %w", err)
	}
	if !ok {
		return fmt.Errorf("mark order event inbox done affected no rows: dedup_key=%s", dedupKey)
	}
	return nil
}

func (c *OrderEventConsumer) processEvent(ctx context.Context, topic string, event *OrderEvent) error {
	switch event.EventType {
	case EventTypeCreated, EventTypeCancelled:
		// 异步失效商品缓存
		if event.SkuID != "" {
			sku, err := c.skuStore.FindOne(ctx, event.SkuID)
			if err != nil {
				return fmt.Errorf("find sku for cache invalidation: %w", err)
			}
			if sku != nil {
				if err := c.redis.Del(ctx, productCacheKey(sku.ProductId)).Err(); err != nil {
					return fmt.Errorf("invalidate product cache: %w", err)
				}
			}
		}
	case EventTypePaid:
		// 占位：可扩展积分、通知、对账
		logx.Infof("order paid event handled: order_id=%s", event.OrderID)
	default:
		return fmt.Errorf("unsupported order event type: %s", event.EventType)
	}

	return nil
}

func productCacheKey(productId string) string {
	return "mall:product:" + productId
}

// Ensure OrderEventConsumer implements service.Service
var _ service.Service = (*OrderEventConsumer)(nil)
