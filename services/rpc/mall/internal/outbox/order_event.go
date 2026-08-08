package outbox

import (
	"fmt"
	"time"

	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
)

// NewOrderEvent creates the canonical transactional outbox record for an
// order event. Callers must persist the returned record with InsertTx in the
// same database transaction as the business state change.
func NewOrderEvent(eventType string, eventTime time.Time, event mq.OrderEvent) (*mall_order_outbox.MallOrderOutbox, error) {
	topic, err := orderEventTopic(eventType)
	if err != nil {
		return nil, err
	}
	if event.OrderId == "" {
		return nil, fmt.Errorf("order event order id is empty")
	}

	eventID := mall_order_outbox.NewMallOrderOutboxId()
	dedupKey := mq.OrderEventDedupKey(event.OrderId, eventType)
	event.EventId = eventID
	event.DedupKey = dedupKey
	// Keep the existing field during the compatibility window. New consumers
	// use dedup_key, while older consumers still receive a stable key.
	event.IdempotencyKey = dedupKey
	payload, err := mq.EncodeOrderEvent(eventType, eventTime, event)
	if err != nil {
		return nil, err
	}

	return &mall_order_outbox.MallOrderOutbox{
		Id:            eventID,
		AggregateType: "order",
		AggregateId:   event.OrderId,
		EventType:     eventType,
		DedupKey:      dedupKey,
		Topic:         topic,
		Tag:           eventType,
		MessageKey:    dedupKey,
		Payload:       string(payload),
		Status:        mall_order_outbox.StatusPending,
		MaxAttempts:   mall_order_outbox.DefaultMaxAttempts,
		NextRetryAt:   eventTime,
		LockedUntil:   eventTime,
	}, nil
}

func orderEventTopic(eventType string) (string, error) {
	switch eventType {
	case mq.EventTypeCreated:
		return mq.TopicOrderCreated, nil
	case mq.EventTypePaid:
		return mq.TopicOrderPaid, nil
	case mq.EventTypeCancelled:
		return mq.TopicOrderCancelled, nil
	default:
		return "", fmt.Errorf("unsupported order event type: %s", eventType)
	}
}
