package outbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgetmatch-sim/services/rpc/mall/internal/mq"
)

func TestNewOrderEventUsesStableBusinessKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, eventType := range []string{mq.EventTypeCreated, mq.EventTypePaid, mq.EventTypeCancelled} {
		t.Run(eventType, func(t *testing.T) {
			row, err := NewOrderEvent(eventType, now, mq.OrderEvent{OrderId: "order-1", UserId: "user-1"})
			require.NoError(t, err)
			wantKey := "order:order-1:" + eventType
			assert.Equal(t, wantKey, row.DedupKey)
			assert.Equal(t, wantKey, row.MessageKey)
			assert.NotEmpty(t, row.Id)

			decoded, err := mq.DecodeOrderEvent([]byte(row.Payload))
			require.NoError(t, err)
			assert.Equal(t, row.Id, decoded.EventId)
			assert.Equal(t, wantKey, decoded.DedupKey)
			assert.Equal(t, wantKey, decoded.IdempotencyKey)
		})
	}
}

func TestNewOrderEventRejectsUnsupportedType(t *testing.T) {
	_, err := NewOrderEvent("unknown", time.Now(), mq.OrderEvent{OrderId: "order-1"})
	require.Error(t, err)
}
