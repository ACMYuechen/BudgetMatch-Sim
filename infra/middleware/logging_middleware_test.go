package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggingMiddleware_Passthrough(t *testing.T) {
	// 验证中间件透传请求到下游 handler，且不修改响应体。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})
	mw := NewLoggingMiddleware("").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestLoggingMiddleware_StatusCodeDefault200(t *testing.T) {
	// 未显式调用 WriteHeader 时，状态码应默认为 200。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	mw := NewLoggingMiddleware("").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoggingMiddleware_StatusCodeError(t *testing.T) {
	// 验证错误状态码被正确捕获。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mw := NewLoggingMiddleware("").Handle(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/fail", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLoggingMiddleware_WriteHeaderOnce(t *testing.T) {
	// 多次调用 WriteHeader 时仅首次状态码生效（与标准库行为一致）。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.WriteHeader(http.StatusBadRequest) // 应被忽略
	})
	mw := NewLoggingMiddleware("").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/once", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestLoggingMiddleware_UserIDEmptySecret(t *testing.T) {
	// 空 secret 时不提取 user_id，请求正常透传。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := NewLoggingMiddleware("").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/noauth", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoggingMiddleware_UserIDNoHeader(t *testing.T) {
	// 有 secret 但无 Authorization 头时正常透传，不崩溃。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := NewLoggingMiddleware("test-secret").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/noheader", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoggingMiddleware_UserIDInvalidToken(t *testing.T) {
	// 有 secret 但 token 无效时正常透传，不崩溃。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := NewLoggingMiddleware("test-secret").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/badtoken", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoggingMiddleware_NoSecretTokenWithBearerPrefix(t *testing.T) {
	// 空 secret 时即使有合法格式的 Bearer token 也正常透传。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := NewLoggingMiddleware("").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/withtoken", nil)
	req.Header.Set("Authorization", "Bearer some.valid.token")
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestResponseWriter_DefaultStatus(t *testing.T) {
	// 从未调用 WriteHeader 时，Write 应默认 200。
	w := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: w}
	n, err := rw.Write([]byte("data"))
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, http.StatusOK, rw.statusCode)
	assert.True(t, rw.wroteHeader)
}

func TestResponseWriter_ExplicitStatus(t *testing.T) {
	// 显式调用 WriteHeader 后，statusCode 不应被后续 Write 覆盖。
	w := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: w}
	rw.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, rw.statusCode)

	_, _ = rw.Write([]byte("not found"))
	assert.Equal(t, http.StatusNotFound, rw.statusCode) // 不变
}

func TestResponseWriter_WriteHeaderIdempotent(t *testing.T) {
	// 多次 WriteHeader 只记录首次。
	w := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: w}
	rw.WriteHeader(http.StatusAccepted)
	rw.WriteHeader(http.StatusBadGateway)
	assert.Equal(t, http.StatusAccepted, rw.statusCode)
}

func TestLoggingMiddleware_ContextPropagation(t *testing.T) {
	// 验证 context 在中间件链中正常传播。
	var capturedKey string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Context().Value("custom-key"); v != nil {
			capturedKey = v.(string)
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := NewLoggingMiddleware("").Handle(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/ctx", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "custom-key", "test-value")  // 注意：测试中 context 需通过 req.WithContext 注入
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, "test-value", capturedKey)
}
