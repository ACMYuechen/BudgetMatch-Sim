package limit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const slidingWindowScript = `
local key = KEYS[1]
local window = tonumber(ARGV[1])
local max = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local member = ARGV[4]

local cutoff = now - window
redis.call("ZREMRANGEBYSCORE", key, 0, cutoff)

local current = redis.call("ZCARD", key)
if current < max then
    redis.call("ZADD", key, now, member)
    redis.call("EXPIRE", key, math.ceil(window / 1000))
    return 1
end

return 0
`

// SlidingWindowLimiter 基于 Redis ZSET 的滑动窗口限流器
type SlidingWindowLimiter struct {
	client redis.UniversalClient
	window time.Duration
	max    int64
}

// NewSlidingWindowLimiter 创建滑动窗口限流器
// window: 时间窗口; max: 窗口内最大请求数
func NewSlidingWindowLimiter(client redis.UniversalClient, window time.Duration, max int64) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		client: client,
		window: window,
		max:    max,
	}
}

// Allow 判断当前请求是否在窗口限制内
func (l *SlidingWindowLimiter) Allow(ctx context.Context, key string) bool {
	now := time.Now().UnixMilli()
	member := fmt.Sprintf("%d:%d", now, time.Now().UnixNano())
	result, err := l.client.Eval(ctx, slidingWindowScript, []string{key},
		l.window.Milliseconds(), l.max, now, member,
	).Result()
	if err != nil {
		fmt.Printf("sliding window limiter error: %v\n", err)
		return false
	}
	return result.(int64) == 1
}
