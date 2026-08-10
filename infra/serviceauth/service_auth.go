// serviceauth 提供为服务之间的短期 JWT 身份认证
package serviceauth

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const (
	ServicePayment = "payment-rpc"
	ServiceMall    = "mall-rpc"
)

// Config 是服务间身份认证配置
type Config struct {
	Secret string `json:"secret"`
}

// Claims 描述服务调用方身份
type Claims struct {
	Service string `json:"service"`
	jwt.RegisteredClaims
}

// GenerateToken 为调用方签发短期服务 JWT
func GenerateToken(caller string, audience string, secret string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(caller) == "" || strings.TrimSpace(audience) == "" || strings.TrimSpace(secret) == "" || ttl <= 0 {
		return "", fmt.Errorf("invalid service token configuration")
	}

	now := time.Now()
	claims := Claims{
		Service: caller,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    caller,
			Subject:   caller,
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken 校验服务 JWT 的签名、调用方、接收方和有效期
func ValidateToken(tokenString string, secret string, expectedCaller string, expectedAudience string) (*Claims, error) {
	if strings.TrimSpace(tokenString) == "" || strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("service token or secret is empty")
	}

	claims := new(Claims)
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)

	token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Method.Alg())
		}
		return []byte(secret), nil
	},
	)
	if err != nil {
		return nil, fmt.Errorf("parse service token failed: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid service token")
	}
	if claims.Service != expectedCaller || claims.Issuer != expectedCaller || claims.Subject != expectedCaller {
		return nil, fmt.Errorf("unexpected service caller")
	}
	if !claims.VerifyAudience(expectedAudience, true) {
		return nil, fmt.Errorf("unexpected service audience")
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil {
		return nil, fmt.Errorf("service token timestamps are missing")
	}
	return claims, nil
}
