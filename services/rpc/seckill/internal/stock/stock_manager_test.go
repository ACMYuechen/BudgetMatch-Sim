package stock

import (
	"budgetmatch-sim/infra/errors"
	"context"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	s := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = r.Close() })
	return s, r
}

func TestStockManager_PreheatAndDeduct(t *testing.T) {
	_, r := setupTestRedis(t)
	sm := NewStockManager(r)

	// preheat 10 stock
	err := sm.Preheat("act1", "sku1", 10, 60)
	assert.NoError(t, err)

	// deduct 3 -> remaining 7
	remain, err := sm.Deduct("act1", "sku1", 3)
	assert.NoError(t, err)
	assert.Equal(t, int64(7), remain)

	// deduct 8 -> not enough
	remain, err = sm.Deduct("act1", "sku1", 8)
	assert.Error(t, err)
	assert.Equal(t, int64(-1), remain)

	// rollback 3 -> 10
	err = sm.Rollback("act1", "sku1", 3)
	assert.NoError(t, err)

	stock, err := sm.GetStock("act1", "sku1")
	assert.NoError(t, err)
	assert.Equal(t, int64(10), stock)
}

func TestStockManager_PreheatAlreadyExists(t *testing.T) {
	_, r := setupTestRedis(t)
	sm := NewStockManager(r)

	err := sm.Preheat("act1", "sku1", 10, 60)
	assert.NoError(t, err)

	err = sm.Preheat("act1", "sku1", 20, 60)
	assert.Error(t, err)
}

func TestStockManager_DeductMissingKey(t *testing.T) {
	_, r := setupTestRedis(t)
	sm := NewStockManager(r)

	remain, err := sm.Deduct("act1", "sku1", 1)
	assert.Error(t, err)
	assert.Equal(t, int64(-2), remain)
}

func TestStockManager_Token(t *testing.T) {
	server, r := setupTestRedis(t)
	sm := NewStockManager(r)
	ctx := context.Background()
	require.NoError(t, sm.SetToken(ctx, "token", "owner", "activity", "sku", time.Second))
	for _, binding := range [][3]string{{"other", "activity", "sku"}, {"owner", "other", "sku"}, {"owner", "activity", "other"}} {
		require.ErrorIs(t, sm.ConsumeToken(ctx, "token", binding[0], binding[1], binding[2]), errors.SeckillTokenInvalid)
	}
	// Invalid callers cannot destroy the owner's capability.
	require.NoError(t, sm.ConsumeToken(ctx, "token", "owner", "activity", "sku"))
	require.ErrorIs(t, sm.ConsumeToken(ctx, "token", "owner", "activity", "sku"), errors.SeckillTokenInvalid)
	require.NoError(t, sm.SetToken(ctx, "expired", "owner", "activity", "sku", time.Second))
	server.FastForward(2 * time.Second)
	require.ErrorIs(t, sm.ConsumeToken(ctx, "expired", "owner", "activity", "sku"), errors.SeckillTokenInvalid)
	require.NoError(t, r.Set(ctx, "seckill:token:legacy", "sku", time.Minute).Err())
	require.ErrorIs(t, sm.ConsumeToken(ctx, "legacy", "owner", "activity", "sku"), errors.SeckillTokenInvalid)
}

func TestStockManager_TokenConcurrentConsumption(t *testing.T) {
	_, r := setupTestRedis(t)
	sm := NewStockManager(r)
	ctx := context.Background()
	require.NoError(t, sm.SetToken(ctx, "token", "owner", "activity", "sku", time.Minute))
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			err := sm.ConsumeToken(ctx, "token", "owner", "activity", "sku")
			if err == nil {
				accepted.Add(1)
			} else {
				assert.ErrorIs(t, err, errors.SeckillTokenInvalid)
			}
		})
	}
	wg.Wait()
	require.Equal(t, int32(1), accepted.Load())
}
