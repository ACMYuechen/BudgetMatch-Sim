// Package middleware 提供通用的 HTTP 中间件，供 cmd/app 和 cmd/admin 复用。
package middleware

import (
	"net/http"
	"strings"
	"time"

	"budgetmatch-sim/infra/auth"

	"github.com/zeromicro/go-zero/core/logx"
)

// LoggingMiddleware 记录每个 HTTP 请求的入口与出口信息，包括方法、路径、状态码、耗时。
// 与 logic 层细粒度业务错误日志形成互补，不做替换。
type LoggingMiddleware struct {
	secret string
}

// NewLoggingMiddleware 创建一个新的请求日志中间件实例。
// secret 为 JWT 签名密钥，非空时从 Authorization 请求头提取 user_id 写入日志；
// 为空时（如 admin 网关走 RPC 鉴权）跳过 user_id。
func NewLoggingMiddleware(secret string) *LoggingMiddleware {
	return &LoggingMiddleware{secret: secret}
}

// Handle 是 go-zero rest.Middleware 的兼容实现。
func (m *LoggingMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		start := time.Now()

		userID := m.extractUserID(r)

		logFields := []logx.LogField{
			logx.Field("method", r.Method),
			logx.Field("path", r.URL.Path),
		}
		if userID != "" {
			logFields = append(logFields, logx.Field("user_id", userID))
		}

		logx.WithContext(ctx).Infow("request start", logFields...)

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next(rw, r)

		duration := time.Since(start).Milliseconds()
		endFields := []logx.LogField{
			logx.Field("method", r.Method),
			logx.Field("path", r.URL.Path),
			logx.Field("status", rw.statusCode),
			logx.Field("duration_ms", duration),
		}
		if userID != "" {
			endFields = append(endFields, logx.Field("user_id", userID))
		}

		if rw.statusCode >= 400 {
			logx.WithContext(ctx).Errorw("request end", endFields...)
		} else {
			logx.WithContext(ctx).Infow("request end", endFields...)
		}
	}
}

// extractUserID 尝试从 Authorization 请求头提取 user_id。
// 仅在中间件持有 JWT secret 时执行，解析失败静默返回空字符串。
func (m *LoggingMiddleware) extractUserID(r *http.Request) string {
	if m.secret == "" {
		return ""
	}

	raw := r.Header.Get("Authorization")
	if raw == "" {
		return ""
	}
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "Bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	if raw == "" {
		return ""
	}

	userID, err := auth.GetUserIdFromToken(raw, m.secret)
	if err != nil {
		return ""
	}
	return userID
}

// responseWriter 包装 http.ResponseWriter，用于捕获实际返回的 HTTP 状态码。
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader 记录首次写入的状态码，然后透传给原始 ResponseWriter。
func (w *responseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write 在首次写入时若未调用 WriteHeader 则默认状态码为 200。
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.statusCode = http.StatusOK
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}
