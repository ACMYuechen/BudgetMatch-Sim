package stock

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"budgetmatch-sim/infra/errors"
)

const (
	stockKeyPrefix = "seckill:stock:%s:%s"
)

// Lua script: deduct stock atomically
// Returns: remaining stock, or -1 if not enough, or -2 if key missing
const deductScript = `
local key = KEYS[1]
local qty = tonumber(ARGV[1])
local exists = redis.call("EXISTS", key)
if exists == 0 then
    return -2
end
local remain = tonumber(redis.call("GET", key))
if remain < qty then
    return -1
end
redis.call("DECRBY", key, qty)
return tonumber(redis.call("GET", key))
`

// Lua script: rollback stock (INCRBY)
const rollbackScript = `
local key = KEYS[1]
local qty = tonumber(ARGV[1])
redis.call("INCRBY", key, qty)
return tonumber(redis.call("GET", key))
`

// Lua script: preheat stock with NX EX
const preheatScript = `
local key = KEYS[1]
local remain = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local ok = redis.call("SET", key, remain, "NX", "EX", ttl)
if ok then
    return 1
end
return 0
`

type StockManager struct {
	redis redis.UniversalClient
}

func NewStockManager(r redis.UniversalClient) *StockManager {
	return &StockManager{redis: r}
}

func stockKey(activityID, skuID string) string {
	return fmt.Sprintf(stockKeyPrefix, activityID, skuID)
}

// Deduct atomically checks and decrements stock.
// Returns remaining stock, or -1 if not enough, or -2 if key missing.
func (sm *StockManager) Deduct(activityID, skuID string, quantity int64) (int64, error) {
	result, err := sm.redis.Eval(context.Background(), deductScript, []string{stockKey(activityID, skuID)}, quantity).Result()
	if err != nil {
		return 0, err
	}
	val := result.(int64)
	if val == -1 {
		return -1, errors.ErrSeckillStockNotEnough
	}
	if val == -2 {
		return -2, errors.ErrSeckillStockNotEnough
	}
	return val, nil
}

// Rollback increments stock back (for order cancellation / failure).
func (sm *StockManager) Rollback(activityID, skuID string, quantity int64) error {
	_, err := sm.redis.Eval(context.Background(), rollbackScript, []string{stockKey(activityID, skuID)}, quantity).Result()
	return err
}

// GetStock returns current stock in Redis.
func (sm *StockManager) GetStock(activityID, skuID string) (int64, error) {
	val, err := sm.redis.Get(context.Background(), stockKey(activityID, skuID)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return val, nil
}

// Preheat sets stock with NX EX (only if key does not exist).
func (sm *StockManager) Preheat(activityID, skuID string, remain int64, ttlSeconds int) error {
	result, err := sm.redis.Eval(context.Background(), preheatScript, []string{stockKey(activityID, skuID)}, remain, ttlSeconds).Result()
	if err != nil {
		return err
	}
	if result.(int64) == 0 {
		return fmt.Errorf("stock key already exists")
	}
	return nil
}

// SetToken creates a seckill token with TTL.
func (sm *StockManager) SetToken(token, skuID string, ttl time.Duration) error {
	return sm.redis.Set(context.Background(), fmt.Sprintf("seckill:token:%s", token), skuID, ttl).Err()
}

// GetToken validates token and returns associated skuID.
func (sm *StockManager) GetToken(token string) (string, error) {
	return sm.redis.Get(context.Background(), fmt.Sprintf("seckill:token:%s", token)).Result()
}

// DelToken deletes a token after use.
func (sm *StockManager) DelToken(token string) error {
	return sm.redis.Del(context.Background(), fmt.Sprintf("seckill:token:%s", token)).Err()
}
