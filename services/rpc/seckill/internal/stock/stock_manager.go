package stock

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

func stockKey(activityId, skuId string) string {
	return fmt.Sprintf(stockKeyPrefix, activityId, skuId)
}

// Deduct atomically checks and decrements stock.
// Returns remaining stock, or -1 if not enough, or -2 if key missing.
func (sm *StockManager) Deduct(activityId, skuId string, quantity int64) (int64, error) {
	result, err := sm.redis.Eval(context.Background(), deductScript, []string{stockKey(activityId, skuId)}, quantity).Result()
	if err != nil {
		return 0, err
	}
	val, ok := toInt64(result)
	if !ok {
		return 0, fmt.Errorf("unexpected deduct script result type %T", result)
	}
	if val == -1 {
		return -1, errors.SeckillStockNotEnough
	}
	if val == -2 {
		return -2, errors.SeckillStockNotEnough
	}
	return val, nil
}

// Rollback increments stock back (for order cancellation / failure).
func (sm *StockManager) Rollback(activityId, skuId string, quantity int64) error {
	_, err := sm.redis.Eval(context.Background(), rollbackScript, []string{stockKey(activityId, skuId)}, quantity).Result()
	return err
}

// GetStock returns current stock in Redis.
func (sm *StockManager) GetStock(activityId, skuId string) (int64, error) {
	val, err := sm.redis.Get(context.Background(), stockKey(activityId, skuId)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return val, nil
}

// Preheat sets stock with NX EX (only if key does not exist).
func (sm *StockManager) Preheat(activityId, skuId string, remain int64, ttlSeconds int) error {
	result, err := sm.redis.Eval(context.Background(), preheatScript, []string{stockKey(activityId, skuId)}, remain, ttlSeconds).Result()
	if err != nil {
		return err
	}
	val, ok := toInt64(result)
	if !ok {
		return fmt.Errorf("unexpected preheat script result type %T", result)
	}
	if val == 0 {
		return fmt.Errorf("stock key already exists")
	}
	return nil
}

// SetToken binds a short-lived capability to one user, activity and SKU.
func (sm *StockManager) SetToken(ctx context.Context, token, userId, activityId, skuId string, ttl time.Duration) error {
	if token == "" || userId == "" || activityId == "" || skuId == "" || ttl <= 0 {
		return errors.SeckillTokenInvalid
	}
	binding, _ := json.Marshal([3]string{userId, activityId, skuId})
	return sm.redis.Set(ctx, "seckill:token:"+token, string(binding), ttl).Err()
}

const consumeTokenScript = `
if redis.call("GET", KEYS[1]) ~= ARGV[1] then
    return 0
end
redis.call("DEL", KEYS[1])
return 1
`

// ConsumeToken does not burn another user's token when any binding mismatches.
// Legacy SKU-only tokens fail closed and expire naturally within 60 seconds.
func (sm *StockManager) ConsumeToken(ctx context.Context, token, userId, activityId, skuId string) error {
	if token == "" || userId == "" || activityId == "" || skuId == "" {
		return errors.SeckillTokenInvalid
	}
	binding, _ := json.Marshal([3]string{userId, activityId, skuId})
	consumed, err := sm.redis.Eval(ctx, consumeTokenScript, []string{"seckill:token:" + token}, string(binding)).Int64()
	if err != nil {
		return err
	}
	if consumed != 1 {
		return errors.SeckillTokenInvalid
	}
	return nil
}

// toInt64 安全地将 Redis/Lua 返回值转换为 int64，避免裸类型断言在异常返回类型时 panic。
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}
