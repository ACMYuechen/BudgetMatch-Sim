package limit

import (
	"context"
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

func TestTokenBucketLimiter_Allow(t *testing.T) {
	_, r := setupTestRedis(t)
	// capacity=2, refill 1 per 100ms
	l := NewTokenBucketLimiter(r, 2, 1, 100*time.Millisecond)
	key := "user:1"

	assert.True(t, l.Allow(context.Background(), key))
	assert.True(t, l.Allow(context.Background(), key))
	assert.False(t, l.Allow(context.Background(), key))

	time.Sleep(110 * time.Millisecond)
	assert.True(t, l.Allow(context.Background(), key))
}

func TestSlidingWindowLimiter_Allow(t *testing.T) {
	_, r := setupTestRedis(t)
	// window=100ms, max=2
	l := NewSlidingWindowLimiter(r, 100*time.Millisecond, 2)
	key := "activity:1"

	assert.True(t, l.Allow(context.Background(), key))
	assert.True(t, l.Allow(context.Background(), key))
	assert.False(t, l.Allow(context.Background(), key))

	time.Sleep(110 * time.Millisecond)
	assert.True(t, l.Allow(context.Background(), key))
}
