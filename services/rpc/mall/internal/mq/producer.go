package mq

import (
	"context"
	"fmt"
	"time"

	"budgetmatch-sim/infra/rocketmq"

	"github.com/zeromicro/go-zero/core/logx"
)

// OrderEventProducer 订单事件生产者
type OrderEventProducer struct {
	producer rocketmq.Producer
}

// NewOrderEventProducer 创建订单事件生产者
func NewOrderEventProducer(p rocketmq.Producer) *OrderEventProducer {
	return &OrderEventProducer{producer: p}
}

func (p *OrderEventProducer) publish(ctx context.Context, topic, eventType string, event OrderEvent) error {
	if p.producer == nil {
		return fmt.Errorf("rocketmq producer is nil")
	}
	body, err := EncodeOrderEvent(eventType, time.Now(), event)
	if err != nil {
		return err
	}

	_, err = p.producer.SendSync(ctx, &rocketmq.Message{
		Topic: topic,
		Body:  body,
		Keys:  []string{event.OrderId},
		Tag:   eventType,
	})
	return err
}

func (p *OrderEventProducer) publishAsync(topic, eventType string, event OrderEvent) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		if err := p.publish(ctx, topic, eventType, event); err != nil {
			logx.WithContext(ctx).Errorf("failed to publish mall order event asynchronously: topic=%s event_type=%s order_id=%s error=%v", topic, eventType, event.OrderId, err)
		}
	}()
}

// PublishCreatedAsync 异步发送订单创建事件，不阻塞主链路
func (p *OrderEventProducer) PublishCreatedAsync(event OrderEvent) {
	p.publishAsync(TopicOrderCreated, EventTypeCreated, event)
}

// PublishPaidAsync 异步发送订单支付事件，不阻塞主链路
func (p *OrderEventProducer) PublishPaidAsync(event OrderEvent) {
	p.publishAsync(TopicOrderPaid, EventTypePaid, event)
}

// PublishCancelledAsync 异步发送订单取消事件，不阻塞主链路
func (p *OrderEventProducer) PublishCancelledAsync(event OrderEvent) {
	p.publishAsync(TopicOrderCancelled, EventTypeCancelled, event)
}
