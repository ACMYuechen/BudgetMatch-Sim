package orderservicelogic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgetmatch-sim/infra/uuid"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
	"budgetmatch-sim/services/rpc/mall/pb"
)

func TestOrderOutboxStatsDeadQueryAndReplay(t *testing.T) {
	tx, serviceContext := newIntegrationServiceContext(t)
	require.NoError(t, tx.Exec("DELETE FROM mall_order_outbox").Error)

	now := time.Now()
	pending := &mall_order_outbox.MallOrderOutbox{
		Id: mall_order_outbox.NewMallOrderOutboxId(), AggregateType: "order", AggregateId: uuid.NewPrefixedShortUUID("order-"), EventType: "created",
		DedupKey: uuid.NewPrefixedShortUUID("dedup-"), Topic: "topic", Tag: "created", MessageKey: "created-key", Payload: `{}`,
		Status: mall_order_outbox.StatusPending, MaxAttempts: 3, NextRetryAt: now, LockedUntil: now,
	}
	dead := &mall_order_outbox.MallOrderOutbox{
		Id: mall_order_outbox.NewMallOrderOutboxId(), AggregateType: "order", AggregateId: uuid.NewPrefixedShortUUID("order-"), EventType: "cancelled",
		DedupKey: uuid.NewPrefixedShortUUID("dedup-"), Topic: "topic", Tag: "cancelled", MessageKey: "cancelled-key", Payload: `{}`,
		Status: mall_order_outbox.StatusDead, Attempts: 3, MaxAttempts: 3, NextRetryAt: now, LockedUntil: now, LastError: "mq unavailable",
	}
	require.NoError(t, serviceContext.OrderOutboxStore.InsertTx(tx, pending))
	require.NoError(t, serviceContext.OrderOutboxStore.InsertTx(tx, dead))

	stats, err := NewGetOrderOutboxStatsLogic(context.Background(), serviceContext).GetOrderOutboxStats(&pb.GetOrderOutboxStatsReq{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, countOutboxStats(stats.Counts, mall_order_outbox.StatusPending, "created"))
	assert.EqualValues(t, 1, countOutboxStats(stats.Counts, mall_order_outbox.StatusDead, "cancelled"))
	assert.NotZero(t, stats.OldestPendingAt)

	list, err := NewListOrderOutboxLogic(context.Background(), serviceContext).ListOrderOutbox(&pb.ListOrderOutboxReq{
		Page: 1, PageSize: 20, Status: int32(mall_order_outbox.StatusDead), AggregateId: dead.AggregateId,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, list.Total)
	require.Len(t, list.List, 1)
	assert.Equal(t, dead.Id, list.List[0].Id)
	assert.Equal(t, dead.DedupKey, list.List[0].DedupKey)

	detail, err := NewGetOrderOutboxLogic(context.Background(), serviceContext).GetOrderOutbox(&pb.GetOrderOutboxReq{Id: dead.Id})
	require.NoError(t, err)
	require.NotNil(t, detail.Event)
	assert.Equal(t, int32(mall_order_outbox.StatusDead), detail.Event.Status)
	assert.Equal(t, "mq unavailable", detail.Event.LastError)

	replay, err := NewReplayOrderOutboxLogic(context.Background(), serviceContext).ReplayOrderOutbox(&pb.ReplayOrderOutboxReq{Id: dead.Id})
	require.NoError(t, err)
	assert.True(t, replay.Success)

	detail, err = NewGetOrderOutboxLogic(context.Background(), serviceContext).GetOrderOutbox(&pb.GetOrderOutboxReq{Id: dead.Id})
	require.NoError(t, err)
	require.NotNil(t, detail.Event)
	assert.Equal(t, int32(mall_order_outbox.StatusPending), detail.Event.Status)
	assert.Zero(t, detail.Event.Attempts)
	assert.Empty(t, detail.Event.LastError)
}

func countOutboxStats(counts []*pb.OrderOutboxStatusCount, status int, eventType string) int64 {
	for _, count := range counts {
		if int(count.Status) == status && count.EventType == eventType {
			return count.Count
		}
	}
	return 0
}
