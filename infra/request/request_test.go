package request

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// ──────────────────────── Context Key 类型 ────────────────────────────────

func TestContextKeyIsTyped(t *testing.T) {
	// ctxKey 是私有强类型，不会与外部 string key 冲突
	ctx := context.WithValue(context.Background(), "token", "string-key-value")
	assert.Empty(t, TryToken(ctx), "string key 不应命中 typed ctxKey")
}

// ──────────────────────── With* / Try* / Must*：正常路径 ──────────────────

func TestWithAndTryToken(t *testing.T) {
	ctx := WithToken(context.Background(), "jwt-token-12345")
	assert.Equal(t, "jwt-token-12345", TryToken(ctx))
}

func TestMustToken_Success(t *testing.T) {
	ctx := WithToken(context.Background(), "jwt-token")
	tk, err := MustToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "jwt-token", tk)
}

func TestMustToken_Missing(t *testing.T) {
	_, err := MustToken(context.Background())
	require.Error(t, err)
}

func TestWithAndTryUserID(t *testing.T) {
	ctx := WithUserID(context.Background(), "usr-abc123")
	assert.Equal(t, "usr-abc123", TryUserID(ctx))
}

func TestMustUserID_Success(t *testing.T) {
	ctx := WithUserID(context.Background(), "usr-abc")
	uid, err := MustUserID(ctx)
	require.NoError(t, err)
	assert.Equal(t, "usr-abc", uid)
}

func TestMustUserID_Missing(t *testing.T) {
	_, err := MustUserID(context.Background())
	require.Error(t, err)
}

func TestWithAndTryRole(t *testing.T) {
	ctx := WithRole(context.Background(), 101)
	assert.Equal(t, int64(101), TryRole(ctx))
}

func TestMustRole_Success(t *testing.T) {
	ctx := WithRole(context.Background(), 100)
	role, err := MustRole(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(100), role)
}

func TestMustRole_Missing(t *testing.T) {
	_, err := MustRole(context.Background())
	require.Error(t, err)
}

func TestWithAndTryRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-001")
	assert.Equal(t, "req-001", TryRequestID(ctx))
}

func TestTryRequestID_Missing(t *testing.T) {
	assert.Empty(t, TryRequestID(context.Background()))
}

func TestWithAndTryClientIP(t *testing.T) {
	ctx := WithClientIP(context.Background(), "192.168.1.1")
	assert.Equal(t, "192.168.1.1", TryClientIP(ctx))
}

func TestTryClientIP_Missing(t *testing.T) {
	assert.Empty(t, TryClientIP(context.Background()))
}

func TestWithAndTryUserAgent(t *testing.T) {
	ctx := WithUserAgent(context.Background(), "TestAgent/1.0")
	assert.Equal(t, "TestAgent/1.0", TryUserAgent(ctx))
}

func TestTryUserAgent_Missing(t *testing.T) {
	assert.Empty(t, TryUserAgent(context.Background()))
}

func TestWithAndTryHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer token-1",
		"X-Request-Id":  "req-1",
	}
	ctx := WithHeaders(context.Background(), headers)
	got := TryHeaders(ctx)
	assert.Equal(t, "Bearer token-1", got["Authorization"])
	assert.Equal(t, "req-1", got["X-Request-Id"])
}

func TestTryHeader(t *testing.T) {
	headers := map[string]string{
		"authorization": "Bearer xyz",
	}
	ctx := WithHeaders(context.Background(), headers)
	assert.Equal(t, "Bearer xyz", TryHeader(ctx, "Authorization"))
	assert.Equal(t, "Bearer xyz", TryHeader(ctx, "authorization")) // 大小写不敏感
	assert.Empty(t, TryHeader(ctx, "X-Request-Id"))
}

func TestTryHeaders_Missing(t *testing.T) {
	assert.Nil(t, TryHeaders(context.Background()))
}

func TestTryHeader_Missing(t *testing.T) {
	assert.Empty(t, TryHeader(context.Background(), "Authorization"))
}

// ──────────────────────── 字段独立性 ──────────────────────────────────────

func TestFieldsAreIndependent(t *testing.T) {
	ctx := context.Background()
	ctx = WithToken(ctx, "t1")
	ctx = WithUserID(ctx, "u1")
	ctx = WithRole(ctx, 100)

	assert.Equal(t, "t1", TryToken(ctx))
	assert.Equal(t, "u1", TryUserID(ctx))
	assert.Equal(t, int64(100), TryRole(ctx))

	// 覆盖写入
	ctx = WithToken(ctx, "t2")
	assert.Equal(t, "t2", TryToken(ctx))
	assert.Equal(t, "u1", TryUserID(ctx)) // UserID 不受影响
}

// ──────────────────────── Token 脱敏 ──────────────────────────────────────

func TestMaskToken(t *testing.T) {
	assert.Equal(t, "", maskToken(""))
	assert.Equal(t, "a***", maskToken("a"))                       // ≤8 个字符：保留首字符
	assert.Equal(t, "a***", maskToken("abcd"))                    // ≤8 个字符：保留首字符
	assert.Equal(t, "1***", maskToken("12345678"))                // =8 个字符：保留首字符
	assert.Equal(t, "12345678***", maskToken("1234567890abcdef")) // >8 个字符：保留前 8
}

// ──────────────────────── Header 规范化 ───────────────────────────────────

func TestCanonicalHeader(t *testing.T) {
	assert.Equal(t, "Authorization", canonicalHeader("authorization"))
	assert.Equal(t, "Authorization", canonicalHeader("AUTHORIZATION"))
	assert.Equal(t, "Authorization", canonicalHeader("  authorization  "))
	assert.Equal(t, "X-Request-Id", canonicalHeader("x-request-id"))
}

// ──────────────────────── cloneAllowedHeaders ─────────────────────────────

func TestCloneAllowedHeaders_Nil(t *testing.T) {
	assert.Nil(t, cloneAllowedHeaders(nil))
	assert.Nil(t, cloneAllowedHeaders(map[string]string{}))
}

func TestCloneAllowedHeaders_FiltersUnknown(t *testing.T) {
	headers := map[string]string{
		"authorization":   "Bearer t",
		"x-custom-header": "should-be-dropped",
	}
	cloned := cloneAllowedHeaders(headers)
	assert.Len(t, cloned, 1)
	assert.Equal(t, "Bearer t", cloned["Authorization"])
}

func TestCloneAllowedHeaders_TrimsWhitespace(t *testing.T) {
	headers := map[string]string{
		"authorization": "  Bearer t  ",
	}
	cloned := cloneAllowedHeaders(headers)
	assert.Equal(t, "Bearer t", cloned["Authorization"])
}

func TestCloneAllowedHeaders_DropsEmpty(t *testing.T) {
	// 白名单 key 存在但值为空或纯空白，不应出现在结果中
	headers := map[string]string{
		"authorization": "",
	}
	assert.Nil(t, cloneAllowedHeaders(headers))

	headers = map[string]string{
		"authorization": "   ",
	}
	assert.Nil(t, cloneAllowedHeaders(headers))
}

func TestCloneAllowedHeaders_IsCopy(t *testing.T) {
	original := map[string]string{"authorization": "Bearer orig"}
	cloned := cloneAllowedHeaders(original)
	original["authorization"] = "Bearer modified"
	assert.Equal(t, "Bearer orig", cloned["Authorization"], "修改原始 map 不应影响 clone")
}

// ──────────────────────── HTTP 解析：FromHTTPRequest ─────────────────────

func TestFromHTTPRequest_Nil(t *testing.T) {
	_, _, err := FromHTTPRequest(context.Background(), nil)
	require.Error(t, err)
}

func TestFromHTTPRequest_NoAuth(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, token, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	assert.Empty(t, token)
}

func TestFromHTTPRequest_BearerToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("Authorization", "Bearer my-jwt-token")
	r.Header.Set("X-Request-Id", "req-123")

	ctx, token, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "my-jwt-token", token)
	assert.Equal(t, "req-123", TryRequestID(ctx))
}

func TestFromHTTPRequest_BearerCaseInsensitive(t *testing.T) {
	for _, prefix := range []string{"Bearer", "bearer", "BEARER", "bEaReR"} {
		r := httptest.NewRequest(http.MethodGet, "/test", nil)
		r.Header.Set("Authorization", prefix+" my-token")
		_, token, err := FromHTTPRequest(context.Background(), r)
		require.NoError(t, err)
		assert.Equal(t, "my-token", token, "prefix=%q", prefix)
	}
}

func TestFromHTTPRequest_BearerWithoutToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("Authorization", "Bearer")
	_, _, err := FromHTTPRequest(context.Background(), r)
	require.Error(t, err)

	r = httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("Authorization", "Bearer ")
	_, _, err = FromHTTPRequest(context.Background(), r)
	require.Error(t, err)
}

func TestFromHTTPRequest_NonBearerToken(t *testing.T) {
	// 非 Bearer 的 Authorization 值应原样保留
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	_, token, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "Basic dXNlcjpwYXNz", token)
}

func TestFromHTTPRequest_DuplicateHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Add("Authorization", "Bearer token-1")
	r.Header.Add("Authorization", "Bearer token-2")
	_, _, err := FromHTTPRequest(context.Background(), r)
	require.Error(t, err)
}

func TestFromHTTPRequest_ClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	r.RemoteAddr = "192.168.1.100:12345"

	ctx, _, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", TryClientIP(ctx))
}

func TestFromHTTPRequest_ClientIP_XRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("X-Real-Ip", "172.16.0.1")
	r.RemoteAddr = "192.168.1.100:12345"

	ctx, _, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "172.16.0.1", TryClientIP(ctx))
}

func TestFromHTTPRequest_ClientIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.RemoteAddr = "10.10.10.10:8080"

	ctx, _, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "10.10.10.10", TryClientIP(ctx))
}

func TestFromHTTPRequest_UserAgent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0 TestBrowser")
	ctx, _, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "Mozilla/5.0 TestBrowser", TryUserAgent(ctx))
}

func TestFromHTTPRequest_HeadersWhitelist(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("Authorization", "Bearer tok")
	r.Header.Set("X-Request-Id", "rid-1")
	r.Header.Set("User-Agent", "UA")
	r.Header.Set("X-Custom", "should-be-dropped")

	ctx, _, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)
	headers := TryHeaders(ctx)
	assert.Len(t, headers, 3)
	assert.Equal(t, "Bearer tok", headers["Authorization"])
	assert.Equal(t, "rid-1", headers["X-Request-Id"])
	assert.Equal(t, "UA", headers["User-Agent"])
	assert.NotContains(t, headers, "X-Custom")
}

// ──────────────────────── gRPC 解析：FromGRPCContext ─────────────────────

func TestFromGRPCContext_NoMetadata(t *testing.T) {
	_, token, err := FromGRPCContext(context.Background())
	require.NoError(t, err)
	assert.Empty(t, token)
}

func TestFromGRPCContext_BearerToken(t *testing.T) {
	md := metadata.Pairs(
		"authorization", "Bearer grpc-token",
		"x-request-id", "greq-1",
		"user-agent", "grpc-agent/1.0",
	)
	inCtx := metadata.NewIncomingContext(context.Background(), md)

	ctx, token, err := FromGRPCContext(inCtx)
	require.NoError(t, err)
	assert.Equal(t, "grpc-token", token)
	assert.Equal(t, "greq-1", TryRequestID(ctx))
	assert.Equal(t, "grpc-agent/1.0", TryUserAgent(ctx))
}

func TestFromGRPCContext_BearerCaseVariations(t *testing.T) {
	tests := []string{"Bearer", "bearer", "BEARER", "bEaReR"}
	for _, prefix := range tests {
		md := metadata.Pairs("authorization", prefix+" grpc-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		_, token, err := FromGRPCContext(ctx)
		require.NoError(t, err, "prefix=%q", prefix)
		assert.Equal(t, "grpc-token", token)
	}
}

func TestFromGRPCContext_DuplicateAuthorization(t *testing.T) {
	md := metadata.MD{
		"authorization": []string{"Bearer t1", "Bearer t2"},
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, _, err := FromGRPCContext(ctx)
	require.Error(t, err)
}

func TestFromGRPCContext_NonBearerToken(t *testing.T) {
	md := metadata.Pairs("authorization", "CustomScheme cred-data")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, token, err := FromGRPCContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "CustomScheme cred-data", token)
}

// ──────────────────────── gRPC 透传：NewOutgoingContext ──────────────────

func TestNewOutgoingContext_Empty(t *testing.T) {
	ctx := NewOutgoingContext(context.Background())
	// 应返回原 Context 或新的空 Context（无 metadata 写入）
	assert.NotNil(t, ctx)
}

func TestNewOutgoingContext_WithTokenAndRequestID(t *testing.T) {
	ctx := context.Background()
	ctx = WithToken(ctx, "out-token")
	ctx = WithRequestID(ctx, "out-rid")

	ctx = NewOutgoingContext(ctx)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{"Bearer out-token"}, md.Get("authorization"))
	assert.Equal(t, []string{"out-rid"}, md.Get("x-request-id"))
}

func TestNewOutgoingContext_OnlyToken(t *testing.T) {
	ctx := WithToken(context.Background(), "token-only")
	ctx = NewOutgoingContext(ctx)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{"Bearer token-only"}, md.Get("authorization"))
}

func TestNewOutgoingContext_OnlyRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "rid-only")
	ctx = NewOutgoingContext(ctx)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{"rid-only"}, md.Get("x-request-id"))
}

// ──────────────────────── TracedRequestID ────────────────────────────────

func TestTracedRequestID_NoTrace(t *testing.T) {
	// 普通 Context 无 trace → 退回随机 uuid（长度 32，无连字符）
	id := TracedRequestID(context.Background())
	assert.Len(t, id, 32)
	assert.False(t, strings.Contains(id, "-"))
}

func TestTracedRequestID_Unique(t *testing.T) {
	id1 := TracedRequestID(context.Background())
	id2 := TracedRequestID(context.Background())
	assert.NotEqual(t, id1, id2)
}

func TestIsZeroHex(t *testing.T) {
	assert.True(t, isZeroHex(""))
	assert.True(t, isZeroHex("0000"))
	assert.True(t, isZeroHex("0000000000000000"))
	assert.False(t, isZeroHex("0001"))
	assert.False(t, isZeroHex("abc0"))
}

// ──────────────────────── normalizedIP ───────────────────────────────────

func TestNormalizedIP(t *testing.T) {
	assert.Equal(t, "192.168.1.1", normalizedIP("192.168.1.1"))
	assert.Equal(t, "192.168.1.1", normalizedIP("  192.168.1.1  "))
	assert.Equal(t, "", normalizedIP("not-an-ip"))
	assert.Equal(t, "", normalizedIP(""))
	assert.Equal(t, "", normalizedIP("999.999.999.999"))
}

// ──────────────────────── 集成：HTTP → Context → 读取 ────────────────────

func TestHTTPToContextRoundTrip(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.Header.Set("Authorization", "Bearer integrated-token")
	r.Header.Set("X-Request-Id", "integrated-req")

	ctx, token, err := FromHTTPRequest(context.Background(), r)
	require.NoError(t, err)

	assert.Equal(t, "integrated-token", token)
	assert.Equal(t, "integrated-token", TryToken(ctx))
	assert.Equal(t, "integrated-req", TryRequestID(ctx))
}

// ──────────────────────── 集成：gRPC → Context → 透传 ────────────────────

func TestGRPCToOutgoingRoundTrip(t *testing.T) {
	md := metadata.Pairs(
		"authorization", "Bearer round-trip-token",
		"x-request-id", "round-req",
	)
	inCtx := metadata.NewIncomingContext(context.Background(), md)

	ctx, _, err := FromGRPCContext(inCtx)
	require.NoError(t, err)

	// FromGRPCContext 已将请求信息注入 ctx，直接透传到 outgoing
	ctx = NewOutgoingContext(ctx)
	outMD, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{"Bearer round-trip-token"}, outMD.Get("authorization"))
	assert.Equal(t, []string{"round-req"}, outMD.Get("x-request-id"))
}
