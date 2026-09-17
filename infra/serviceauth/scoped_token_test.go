package serviceauth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func TestScopedToken(t *testing.T) {
	token, err := GenerateScopedToken(ServiceAgent, ServiceMall, PurposeProductIndexRead, testServiceSecret, time.Minute)
	require.NoError(t, err)
	claims, err := ValidateScopedToken(token, testServiceSecret, ServiceAgent, ServiceMall, PurposeProductIndexRead)
	require.NoError(t, err)
	require.Equal(t, PurposeProductIndexRead, claims.Purpose)
	require.Equal(t, time.Minute, claims.ExpiresAt.Sub(claims.IssuedAt.Time))

	for _, tc := range []struct {
		name   string
		mutate func(*Claims)
	}{
		{"wrong purpose", func(c *Claims) { c.Purpose = "order:write" }},
		{"missing purpose", func(c *Claims) { c.Purpose = "" }},
		{"wrong caller", func(c *Claims) { c.Service = ServicePayment }},
		{"wrong issuer", func(c *Claims) { c.Issuer = ServicePayment }},
		{"wrong subject", func(c *Claims) { c.Subject = ServicePayment }},
		{"wrong audience", func(c *Claims) { c.Audience = jwt.ClaimStrings{ServicePayment} }},
		{"missing expiry", func(c *Claims) { c.ExpiresAt = nil }},
		{"missing issued at", func(c *Claims) { c.IssuedAt = nil }},
		{"missing not before", func(c *Claims) { c.NotBefore = nil }},
		{"missing id", func(c *Claims) { c.ID = "" }},
		{"long lifetime", func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }},
		{"expired", func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute)) }},
		{"future issued at", func(c *Claims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }},
		{"future not before", func(c *Claims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := *claims
			tc.mutate(&bad)
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, bad).SignedString([]byte(testServiceSecret))
			require.NoError(t, err)
			got, err := ValidateScopedToken(signed, testServiceSecret, ServiceAgent, ServiceMall, PurposeProductIndexRead)
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		signed, err := jwt.NewWithClaims(method, claims).SignedString([]byte(testServiceSecret))
		require.NoError(t, err)
		_, err = ValidateScopedToken(signed, testServiceSecret, ServiceAgent, ServiceMall, PurposeProductIndexRead)
		require.Error(t, err)
	}
	_, err = ValidateScopedToken(token, "wrong-secret", ServiceAgent, ServiceMall, PurposeProductIndexRead)
	require.Error(t, err)
	_, err = ValidateScopedToken(token, testServiceSecret, ServiceAgent, ServiceMall, "")
	require.Error(t, err)
}

func TestGenerateScopedTokenRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		caller, audience, purpose, secret string
		ttl                               time.Duration
	}{
		{"", ServiceMall, PurposeProductIndexRead, testServiceSecret, time.Minute},
		{ServiceAgent, "", PurposeProductIndexRead, testServiceSecret, time.Minute},
		{ServiceAgent, ServiceMall, " ", testServiceSecret, time.Minute},
		{ServiceAgent, ServiceMall, PurposeProductIndexRead, "", time.Minute},
		{ServiceAgent, ServiceMall, PurposeProductIndexRead, testServiceSecret, 0},
		{ServiceAgent, ServiceMall, PurposeProductIndexRead, testServiceSecret, -time.Minute},
		{ServiceAgent, ServiceMall, PurposeProductIndexRead, testServiceSecret, MaxScopedTokenTTL + time.Second},
	} {
		token, err := GenerateScopedToken(tc.caller, tc.audience, tc.purpose, tc.secret, tc.ttl)
		require.Error(t, err)
		require.Empty(t, token)
	}
}

func TestDedicatedSecretValidation(t *testing.T) {
	require.NoError(t, ValidateDedicatedSecret(testServiceSecret, "user-secret", "payment-secret"))
	for _, secret := range []string{"", "short", strings.Repeat(" ", 32), " " + testServiceSecret, testServiceSecret + " ", testServiceSecret} {
		err := ValidateDedicatedSecret(secret, testServiceSecret)
		require.Error(t, err)
		require.NotContains(t, err.Error(), testServiceSecret)
	}
}
