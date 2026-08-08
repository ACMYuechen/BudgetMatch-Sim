package mall_order_event_inbox

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

func TestInboxDeduplicationAndLeaseRecovery(t *testing.T) {
	dsn := os.Getenv("BUDGETMATCH_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set BUDGETMATCH_TEST_POSTGRES_DSN to run inbox integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, NewMallOrderEventInboxModel(db).CreateTable())
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback().Error })
	store := NewMallOrderEventInboxModel(tx)
	now := time.Now()

	doneKey := "order:" + uuid.NewPrefixedShortUUID("test-") + ":paid"
	claim, err := store.Claim(context.Background(), "consumer-1", doneKey, "paid", now, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, Claimed, claim)
	ok, err := store.MarkDone(context.Background(), "consumer-1", doneKey, now.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, ok)
	claim, err = store.Claim(context.Background(), "consumer-1", doneKey, "paid", now.Add(2*time.Second), now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, AlreadyProcessed, claim)

	leaseKey := "order:" + uuid.NewPrefixedShortUUID("test-") + ":created"
	claim, err = store.Claim(context.Background(), "consumer-1", leaseKey, "created", now, now.Add(time.Second))
	require.NoError(t, err)
	assert.Equal(t, Claimed, claim)
	claim, err = store.Claim(context.Background(), "consumer-1", leaseKey, "created", now.Add(500*time.Millisecond), now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, AlreadyProcessing, claim)
	claim, err = store.Claim(context.Background(), "consumer-1", leaseKey, "created", now.Add(2*time.Second), now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, Claimed, claim)
}
