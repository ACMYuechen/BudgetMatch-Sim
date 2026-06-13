package limit

import "context"

// Limiter 通用限流器接口
type Limiter interface {
	Allow(ctx context.Context, key string) bool
}
