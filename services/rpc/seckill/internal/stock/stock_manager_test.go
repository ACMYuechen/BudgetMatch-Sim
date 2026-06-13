package stock

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	s := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: s.Addr()})
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
	_, r := setupTestRedis(t)
	sm := NewStockManager(r)

	err := sm.SetToken("tok1", "sku1", time.Second)
	assert.NoError(t, err)

	sku, err := sm.GetToken("tok1")
	assert.NoError(t, err)
	assert.Equal(t, "sku1", sku)

	err = sm.DelToken("tok1")
	assert.NoError(t, err)

	_, err = sm.GetToken("tok1")
	assert.Equal(t, redis.Nil, err)
}
