package mq

import (
	"encoding/json"
	"fmt"
	"time"
)

// Topic 常量定义
const (
	TopicOrderCreated   = "mall_order_created"
	TopicOrderPaid      = "mall_order_paid"
	TopicOrderCancelled = "mall_order_cancelled"
)

// EventType 事件类型
const (
	EventTypeCreated   = "created"
	EventTypePaid      = "paid"
	EventTypeCancelled = "cancelled"
)

// OrderEvent 订单事件消息体
type OrderEvent struct {
	OrderId        string `json:"order_id"`
	UserId         string `json:"user_id"`
	SkuId          string `json:"sku_id"`
	Quantity       int64  `json:"quantity"`
	Status         int32  `json:"status"`
	EventType      string `json:"event_type"`
	EventTime      int64  `json:"event_time"`
	IdempotencyKey string `json:"idempotency_key"`
}

// EncodeOrderEvent 序列化订单事件
func EncodeOrderEvent(eventType string, eventTime time.Time, event OrderEvent) ([]byte, error) {
	event.EventType = eventType
	event.EventTime = eventTime.Unix()
	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal order event failed: %w", err)
	}
	return body, nil
}

// DecodeOrderEvent 反序列化订单事件
func DecodeOrderEvent(body []byte) (*OrderEvent, error) {
	var event OrderEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("unmarshal order event failed: %w", err)
	}
	return &event, nil
}
