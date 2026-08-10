package serviceauth

import (
	userauth "budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/role"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testServiceSecret = "service-secret-for-unit-testing-at-least-32-bytes"

// TestGenerateAndValidateToken 验证合法服务 Token 可以通过校验
func TestGenerateAndValidateToken(t *testing.T) {
	token, err := GenerateToken(ServicePayment, ServiceMall, testServiceSecret, time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := ValidateToken(token, testServiceSecret, ServicePayment, ServiceMall)
	require.NoError(t, err)
	assert.Equal(t, ServicePayment, claims.Service)
	assert.Equal(t, ServicePayment, claims.Issuer)
	assert.Equal(t, ServicePayment, claims.Subject)
	assert.True(t, claims.VerifyAudience(ServiceMall, true))
	assert.NotNil(t, claims.IssuedAt)
	assert.NotNil(t, claims.ExpiresAt)
	assert.NotEmpty(t, claims.ID)
}

// TestValidateTokenRejectsInvalidIdentity 验证错误身份，接收方和密钥均被拒绝
func TestValidateTokenRejectsInvalidIdentity(t *testing.T) {
	validToken, err := GenerateToken(ServicePayment, ServiceMall, testServiceSecret, time.Minute)
	require.NoError(t, err)

	wrongCallerToken, err := GenerateToken("agent-rpc", ServiceMall, testServiceSecret, time.Minute)
	require.NoError(t, err)

	wrongAudienceToken, err := GenerateToken(ServicePayment, "payment-rpc", testServiceSecret, time.Minute)
	require.NoError(t, err)

	tests := []struct {
		name     string
		token    string
		secret   string
		caller   string
		audience string
	}{
		{
			name:     "wrong secret",
			token:    validToken,
			secret:   "wrong-service-secret",
			caller:   ServicePayment,
			audience: ServiceMall,
		},
		{
			name:     "wrong caller",
			token:    wrongCallerToken,
			secret:   testServiceSecret,
			caller:   ServicePayment,
			audience: ServiceMall,
		},
		{
			name:     "wrong audience",
			token:    wrongAudienceToken,
			secret:   testServiceSecret,
			caller:   ServicePayment,
			audience: ServiceMall,
		},
		{
			name:     "empty token",
			token:    "",
			secret:   testServiceSecret,
			caller:   ServicePayment,
			audience: ServiceMall,
		},
		{
			name:     "empty secret",
			token:    validToken,
			secret:   "",
			caller:   ServicePayment,
			audience: ServiceMall,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := ValidateToken(tt.token, tt.secret, tt.caller, tt.audience)
			assert.Error(t, err)
			assert.Nil(t, claims)
		})
	}
}

// TestValidateTokenRejectsExpiredToken 验证过期服务 Token 被拒绝
func TestValidateTokenRejectsExpiredToken(t *testing.T) {
	now := time.Now()
	claims := Claims{
		Service: ServicePayment,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    ServicePayment,
			Subject:   ServicePayment,
			Audience:  jwt.ClaimStrings{ServiceMall},
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testServiceSecret))
	require.NoError(t, err)

	result, err := ValidateToken(token, testServiceSecret, ServicePayment, ServiceMall)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestValidateTokenRejectsUserJWT 验证普通用户 JWT 不能冒充服务 Token
func TestValidateTokenRejectsUserJWT(t *testing.T) {
	userToken, err := userauth.GenerateToken("user-1", testServiceSecret, 3600, role.RoleUser)
	require.NoError(t, err)
	claims, err := ValidateToken(userToken, testServiceSecret, ServiceMall, ServiceMall)
	assert.Error(t, err)
	assert.Nil(t, claims)
}
