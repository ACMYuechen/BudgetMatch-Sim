package outbox

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
)

func TestRetryBackoff(t *testing.T) {
	base := time.Second
	maximum := 5 * time.Second
	assert.Equal(t, time.Second, retryBackoff(1, base, maximum))
	assert.Equal(t, 2*time.Second, retryBackoff(2, base, maximum))
	assert.Equal(t, 4*time.Second, retryBackoff(3, base, maximum))
	assert.Equal(t, maximum, retryBackoff(4, base, maximum))
	assert.Equal(t, maximum, retryBackoff(20, base, maximum))
}

func TestTruncateError(t *testing.T) {
	assert.Equal(t, " error ", truncateError(" error "))
	assert.Len(t, truncateError(strings.Repeat("x", 3000)), 2048)
}

func TestPublishMarksSent(t *testing.T) {
	store := &fakeStore{}
	producer := &fakeProducer{}
	dispatcher := NewDispatcher(store, producer, DefaultConfig())
	event := processingEvent(1, 3)

	dispatcher.publish(event)

	require.True(t, store.sent)
	assert.Equal(t, event.Id, store.sentId)
	assert.Equal(t, event.Attempts, store.sentAttempt)
	assert.Nil(t, store.retry)
	assert.Nil(t, store.dead)
}

func TestPublishFailureSchedulesRetry(t *testing.T) {
	store := &fakeStore{}
	producer := &fakeProducer{sendErr: stderrors.New("mq unavailable")}
	dispatcher := NewDispatcher(store, producer, DefaultConfig())
	event := processingEvent(1, 3)

	dispatcher.publish(event)

	require.NotNil(t, store.retry)
	assert.Equal(t, event.Id, store.retry.Id)
	assert.Equal(t, event.Attempts, store.retry.Attempt)
	assert.Contains(t, store.retry.LastError, "mq unavailable")
	assert.False(t, store.sent)
	assert.Nil(t, store.dead)
}

func TestPublishFailureMarksDeadAtMaxAttempts(t *testing.T) {
	store := &fakeStore{}
	producer := &fakeProducer{sendErr: stderrors.New("mq unavailable")}
	dispatcher := NewDispatcher(store, producer, DefaultConfig())
	event := processingEvent(3, 3)

	dispatcher.publish(event)

	require.NotNil(t, store.dead)
	assert.Equal(t, event.Id, store.dead.Id)
	assert.Equal(t, event.Attempts, store.dead.Attempt)
	assert.False(t, store.sent)
	assert.Nil(t, store.retry)
}

func processingEvent(attempts, maxAttempts int) *mall_order_outbox.MallOrderOutbox {
	return &mall_order_outbox.MallOrderOutbox{Id: "event-1", AggregateId: "order-1", Topic: "topic", Tag: "paid", MessageKey: "order-1", Payload: `{}`, Status: mall_order_outbox.StatusProcessing, Attempts: attempts, MaxAttempts: maxAttempts}
}

type fakeStore struct {
	sent        bool
	sentId      string
	sentAttempt int
	retry       *mall_order_outbox.MarkRetryReq
	dead        *mall_order_outbox.MarkDeadReq
}

func (s *fakeStore) ClaimBatch(context.Context, int, time.Time, time.Time) ([]mall_order_outbox.MallOrderOutbox, int64, error) {
	return nil, 0, nil
}

func (s *fakeStore) MarkSent(_ context.Context, id string, attempt int, _ time.Time) (bool, error) {
	s.sent = true
	s.sentId = id
	s.sentAttempt = attempt
	return true, nil
}

func (s *fakeStore) MarkRetry(_ context.Context, req *mall_order_outbox.MarkRetryReq) (bool, error) {
	s.retry = req
	return true, nil
}

func (s *fakeStore) MarkDead(_ context.Context, req *mall_order_outbox.MarkDeadReq) (bool, error) {
	s.dead = req
	return true, nil
}

type fakeProducer struct {
	sendErr error
}

func (p *fakeProducer) SendSync(context.Context, *rocketmq.Message) (*rocketmq.SendResult, error) {
	if p.sendErr != nil {
		return nil, p.sendErr
	}
	return &rocketmq.SendResult{MsgId: "msg-1"}, nil
}

func (p *fakeProducer) Shutdown() error {
	return nil
}
