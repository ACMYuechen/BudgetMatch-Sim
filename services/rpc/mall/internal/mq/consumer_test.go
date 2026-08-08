package mq

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_event_inbox"
)

func TestNormalizeOrderEventDedupKeySupportsLegacyPayload(t *testing.T) {
	event := &OrderEvent{OrderId: "order-1", EventType: EventTypePaid, IdempotencyKey: "legacy-random-id"}
	assert.Equal(t, "order:order-1:paid", NormalizeOrderEventDedupKey(event))
}

func TestConsumerProcessesDuplicateEventOnce(t *testing.T) {
	inbox := &fakeInbox{}
	consumer := NewOrderEventConsumer(rocketMQConfigForTest(), nil, nil, inbox)
	body, err := EncodeOrderEvent(EventTypePaid, time.Now(), OrderEvent{OrderId: "order-1"})
	require.NoError(t, err)

	require.NoError(t, consumer.handleMessage(context.Background(), TopicOrderPaid, body))
	require.NoError(t, consumer.handleMessage(context.Background(), TopicOrderPaid, body))

	assert.Equal(t, 1, inbox.doneCalls)
}

func rocketMQConfigForTest() rocketmq.Config {
	return rocketmq.Config{}
}

type fakeInbox struct {
	processed bool
	doneCalls int
}

func (f *fakeInbox) CreateTable() error { return nil }

func (f *fakeInbox) Claim(context.Context, string, string, string, time.Time, time.Time) (mall_order_event_inbox.ClaimResult, error) {
	if f.processed {
		return mall_order_event_inbox.AlreadyProcessed, nil
	}
	return mall_order_event_inbox.Claimed, nil
}

func (f *fakeInbox) MarkDone(context.Context, string, string, time.Time) (bool, error) {
	f.processed = true
	f.doneCalls++
	return true, nil
}

func (f *fakeInbox) MarkRetry(context.Context, string, string, string, time.Time) (bool, error) {
	return true, nil
}
