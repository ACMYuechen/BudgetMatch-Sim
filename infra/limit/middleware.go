package limit

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
)

// KeyExtractor 从 HTTP 请求中提取限流 key
type KeyExtractor func(r *http.Request) string

// NewRateLimitMiddleware 返回 REST 限流中间件
// limiter: 限流器; keyExtractor: 从请求中提取 key 的函数
func NewRateLimitMiddleware(limiter Limiter, keyExtractor KeyExtractor) rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := keyExtractor(r)
			if key == "" {
				next(w, r)
				return
			}
			if !limiter.Allow(r.Context(), key) {
				logx.WithContext(r.Context()).Errorf("rate limit exceeded: key=%s", key)
				http.Error(w, errors.TooManyRequests.Error(), http.StatusTooManyRequests)
				return
			}
			next(w, r)
		}
	}
}
