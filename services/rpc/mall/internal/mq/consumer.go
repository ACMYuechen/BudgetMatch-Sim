package mq

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"

	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
)

const consumerGroupName = "mall-order-consumer"

// OrderEventConsumer 订单事件消费者，监听 RocketMQ 订单相关 Topic
type OrderEventConsumer struct {
	cfg      rocketmq.Config
	skuStore product_skus.ProductSkusModel
	redis    redis.UniversalClient
	consumer rocketmq.Consumer
	quit     chan struct{}
}

// NewOrderEventConsumer 创建订单事件消费者
func NewOrderEventConsumer(cfg rocketmq.Config, skuStore product_skus.ProductSkusModel, rds redis.UniversalClient) *OrderEventConsumer {
	return &OrderEventConsumer{
		cfg:      cfg,
		skuStore: skuStore,
		redis:    rds,
		quit:     make(chan struct{}),
	}
}

func (c *OrderEventConsumer) Start() {
	if len(c.cfg.NameServers) == 0 {
		logx.Info("rocketmq not configured, order event consumer not started")
		return
	}

	cfg := c.cfg
	cfg.GroupName = consumerGroupName

	consumer, err := rocketmq.NewConsumer(cfg)
	if err != nil {
		logx.Errorf("create rocketmq consumer failed: %v", err)
		return
	}
	c.consumer = consumer

	topics := []string{TopicOrderCreated, TopicOrderPaid, TopicOrderCancelled}
	for _, topic := range topics {
		if err := consumer.Subscribe(topic, "", func(ctx context.Context, body []byte) error {
			return c.handleMessage(ctx, topic, body)
		}); err != nil {
			logx.Errorf("subscribe topic %s failed: %v", topic, err)
			return
		}
	}

	go func() {
		if err := consumer.Start(); err != nil {
			logx.Errorf("start rocketmq consumer failed: %v", err)
		}
	}()

	logx.Info("order event consumer started")
	<-c.quit
}

func (c *OrderEventConsumer) Stop() {
	if c.consumer != nil {
		_ = c.consumer.Shutdown()
	}
	close(c.quit)
	logx.Info("order event consumer stopped")
}

func (c *OrderEventConsumer) handleMessage(ctx context.Context, topic string, body []byte) error {
	event, err := DecodeOrderEvent(body)
	if err != nil {
		logx.Errorf("decode order event failed: %v, topic=%s, body=%s", err, topic, string(body))
		// 返回 nil 避免 poison message 无限重试
		return nil
	}

	logx.Infof("handle order event: topic=%s, order_id=%s, user_id=%s, sku_id=%s, qty=%d",
		topic, event.OrderId, event.UserId, event.SkuId, event.Quantity)

	switch event.EventType {
	case EventTypeCreated, EventTypeCancelled:
		// 异步失效商品缓存
		if event.SkuId != "" {
			sku, err := c.skuStore.FindOne(ctx, event.SkuId)
			if err == nil && sku != nil {
				_ = c.redis.Del(ctx, productCacheKey(sku.ProductId))
			}
		}
	case EventTypePaid:
		// 占位：可扩展积分、通知、对账
		logx.Infof("order paid event handled: order_id=%s", event.OrderId)
	}

	return nil
}

func productCacheKey(productId string) string {
	return "mall:product:" + productId
}

// Ensure OrderEventConsumer implements service.Service
var _ service.Service = (*OrderEventConsumer)(nil)
