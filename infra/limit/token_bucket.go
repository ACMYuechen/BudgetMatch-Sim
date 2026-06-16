package limit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const tokenBucketScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local interval = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

local state = redis.call("HMGET", key, "tokens", "last_time")
local tokens = tonumber(state[1])
local lastTime = tonumber(state[2])

if tokens == nil then
    tokens = capacity
    lastTime = now
end

local elapsed = now - lastTime
local refill = math.floor(elapsed / interval) * rate
if refill > 0 then
    tokens = math.min(capacity, tokens + refill)
    lastTime = now
end

if tokens >= 1 then
    tokens = tokens - 1
    redis.call("HMSET", key, "tokens", tokens, "last_time", lastTime)
    redis.call("EXPIRE", key, 60)
    return 1
else
    redis.call("HMSET", key, "tokens", tokens, "last_time", lastTime)
    redis.call("EXPIRE", key, 60)
    return 0
end
`

// TokenBucketLimiter 基于 Redis Hash 的令牌桶限流器
type TokenBucketLimiter struct {
	client   redis.UniversalClient
	capacity int64
	rate     int64
	interval time.Duration
}

// NewTokenBucketLimiter 创建令牌桶限流器
// capacity: 桶容量; rate: 每次 refill 令牌数; interval: refill 间隔
func NewTokenBucketLimiter(client redis.UniversalClient, capacity, rate int64, interval time.Duration) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		client:   client,
		capacity: capacity,
		rate:     rate,
		interval: interval,
	}
}

// SetCapacity 动态修改桶容量
func (l *TokenBucketLimiter) SetCapacity(capacity int64) {
	l.capacity = capacity
}

// SetRate 动态修改每次 refill 令牌数
func (l *TokenBucketLimiter) SetRate(rate int64) {
	l.rate = rate
}

// SetInterval 动态修改 refill 间隔
func (l *TokenBucketLimiter) SetInterval(interval time.Duration) {
	l.interval = interval
}

// Allow 尝试获取一个令牌，返回是否允许通过
func (l *TokenBucketLimiter) Allow(ctx context.Context, key string) bool {
	now := time.Now().UnixMilli()
	result, err := l.client.Eval(ctx, tokenBucketScript, []string{key},
		l.capacity, l.rate, l.interval.Milliseconds(), now,
	).Result()
	if err != nil {
		fmt.Printf("token bucket limiter error: %v\n", err)
		return false
	}
	return result.(int64) == 1
}
