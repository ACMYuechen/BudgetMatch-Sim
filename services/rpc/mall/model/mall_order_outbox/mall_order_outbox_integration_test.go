package mall_order_outbox

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"budgetmatch-sim/infra/uuid"
)

func TestReplayDeadAndQueryStats(t *testing.T) {
	dsn := os.Getenv("BUDGETMATCH_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set BUDGETMATCH_TEST_POSTGRES_DSN to run outbox model integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, NewMallOrderOutboxModel(db).CreateTable())
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback().Error })
	store := NewMallOrderOutboxModel(tx)
	require.NoError(t, tx.Exec("DELETE FROM mall_order_outbox").Error)

	now := time.Now()
	pending := &MallOrderOutbox{
		Id: NewMallOrderOutboxId(), AggregateType: "order", AggregateId: uuid.NewPrefixedShortUUID("order-"), EventType: "created",
		DedupKey: uuid.NewPrefixedShortUUID("dedup-"), Topic: "topic", Tag: "created", MessageKey: "key", Payload: `{}`,
		Status: StatusPending, Attempts: 0, MaxAttempts: 3, NextRetryAt: now, LockedUntil: now,
	}
	dead := &MallOrderOutbox{
		Id: NewMallOrderOutboxId(), AggregateType: "order", AggregateId: uuid.NewPrefixedShortUUID("order-"), EventType: "cancelled",
		DedupKey: uuid.NewPrefixedShortUUID("dedup-"), Topic: "topic", Tag: "cancelled", MessageKey: "key", Payload: `{}`,
		Status: StatusDead, Attempts: 3, MaxAttempts: 3, NextRetryAt: now, LockedUntil: now, LastError: "mq unavailable",
	}
	require.NoError(t, store.InsertTx(tx, pending))
	require.NoError(t, store.InsertTx(tx, dead))

	counts, oldest, err := store.GetStats(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, countForStatus(counts, StatusPending, "created"))
	assert.EqualValues(t, 1, countForStatus(counts, StatusDead, "cancelled"))
	assert.False(t, oldest.IsZero())

	deadList, total, err := store.ListFiltered(context.Background(), ListFilteredReq{Page: 1, Size: 20, Status: StatusDead, AggregateId: dead.AggregateId})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, deadList, 1)
	assert.Equal(t, dead.Id, deadList[0].Id)
	assert.Equal(t, dead.DedupKey, deadList[0].DedupKey)
	assert.Equal(t, "mq unavailable", deadList[0].LastError)

	ok, err := store.ReplayDead(context.Background(), dead.Id, now.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, ok)
	stored, err := store.FindOne(context.Background(), dead.Id)
	require.NoError(t, err)
	assert.Equal(t, StatusPending, stored.Status)
	assert.Zero(t, stored.Attempts)
	assert.Empty(t, stored.LastError)

	ok, err = store.ReplayDead(context.Background(), dead.Id, now.Add(2*time.Second))
	require.NoError(t, err)
	assert.False(t, ok)

	list, total, err := store.ListFiltered(context.Background(), ListFilteredReq{Page: 1, Size: 20, Status: StatusPending, AggregateId: dead.AggregateId})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, list, 1)
	assert.Equal(t, dead.Id, list[0].Id)

	deadList, total, err = store.ListFiltered(context.Background(), ListFilteredReq{Page: 1, Size: 20, Status: StatusDead, AggregateId: dead.AggregateId})
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, deadList)

	counts, _, err = store.GetStats(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, countForStatus(counts, StatusPending, "created"))
	assert.EqualValues(t, 1, countForStatus(counts, StatusPending, "cancelled"))
	assert.EqualValues(t, 0, countForStatus(counts, StatusDead, "cancelled"))
}

func countForStatus(counts []StatusCount, status int, eventType string) int64 {
	for _, count := range counts {
		if count.Status == status && count.EventType == eventType {
			return count.Count
		}
	}
	return 0
}
