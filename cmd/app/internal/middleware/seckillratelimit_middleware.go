package middleware

import (
	"net/http"
	"strings"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/limit"

	"github.com/zeromicro/go-zero/core/logx"
)

type SeckillRateLimitMiddleware struct {
	limiter limit.Limiter
}

func NewSeckillRateLimitMiddleware(limiter limit.Limiter) *SeckillRateLimitMiddleware {
	return &SeckillRateLimitMiddleware{limiter: limiter}
}

func (m *SeckillRateLimitMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// 仅对 token 和 orders 路径限流
		if !strings.Contains(path, "/token") && !strings.Contains(path, "/orders") {
			next(w, r)
			return
		}

		userId := r.Context().Value("user_id")
		if userId == nil {
			next(w, r)
			return
		}

		key := "seckill:" + userId.(string) + ":" + path
		if !m.limiter.Allow(r.Context(), key) {
			logx.WithContext(r.Context()).Errorf("seckill rate limit exceeded: key=%s", key)
			http.Error(w, errors.TooManyRequests.Error(), http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}
