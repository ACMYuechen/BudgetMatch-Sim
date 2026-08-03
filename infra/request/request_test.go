package request

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
