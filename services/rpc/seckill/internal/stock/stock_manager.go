package stock

import (
	"context"
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
	val, ok := toInt64(result)
	if !ok {
		return fmt.Errorf("unexpected preheat script result type %T", result)
	}
	if val == 0 {
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

// ConsumeToken 原子地校验并删除 token（GETDEL），返回其绑定的 skuID。
// 相比 GetToken + DelToken 两步操作，这里保证 token 一次性消费，
// 避免并发场景下同一 token 在删除前被重复读取使用。
func (sm *StockManager) ConsumeToken(token string) (string, error) {
	return sm.redis.GetDel(context.Background(), fmt.Sprintf("seckill:token:%s", token)).Result()
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
