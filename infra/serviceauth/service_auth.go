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
	ServiceAgent   = "agent-rpc"

	PurposeProductIndexRead = "product-index:read"
	MaxScopedTokenTTL       = 5 * time.Minute
)

// Config 是服务间身份认证配置
type Config struct {
	Secret string `json:"secret"`
}

// Claims 描述服务调用方身份
type Claims struct {
	Service string `json:"service"`
	Purpose string `json:"purpose,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken 为调用方签发短期服务 JWT
func GenerateToken(caller string, audience string, secret string, ttl time.Duration) (string, error) {
	return generateToken(caller, audience, "", secret, ttl)
}

// GenerateScopedToken 签发有明确用途、最长五分钟有效期的服务 Token。
// 旧 Payment Token 的签发接口与验证规则保持不变。
func GenerateScopedToken(caller, audience, purpose, secret string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(purpose) == "" || ttl > MaxScopedTokenTTL {
		return "", fmt.Errorf("invalid scoped service token configuration")
	}
	return generateToken(caller, audience, purpose, secret, ttl)
}

func generateToken(caller, audience, purpose, secret string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(caller) == "" || strings.TrimSpace(audience) == "" || strings.TrimSpace(secret) == "" || ttl <= 0 {
		return "", fmt.Errorf("invalid service token configuration")
	}

	now := time.Now()
	claims := Claims{
		Service: caller,
		Purpose: purpose,
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

// ValidateScopedToken 除身份/签名/有效期外，强制匹配用途和短期有效期上限。
func ValidateScopedToken(token, secret, caller, audience, purpose string) (*Claims, error) {
	if strings.TrimSpace(purpose) == "" {
		return nil, fmt.Errorf("service token purpose is required")
	}
	claims, err := ValidateToken(token, secret, caller, audience)
	if err != nil {
		return nil, err
	}
	if claims.Purpose != purpose {
		return nil, fmt.Errorf("unexpected service token purpose")
	}
	ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if ttl <= 0 || ttl > MaxScopedTokenTTL || claims.NotBefore == nil || claims.ID == "" {
		return nil, fmt.Errorf("invalid scoped service token lifetime or identity")
	}
	return claims, nil
}

// ValidateDedicatedSecret 校验新服务密钥的基本长度和已知凭据隔离；不输出密钥。
// 长度检查不能证明随机性，部署时仍必须使用密码学安全的随机密钥。
func ValidateDedicatedSecret(secret string, otherSecrets ...string) error {
	if len(secret) < 32 || secret != strings.TrimSpace(secret) {
		return fmt.Errorf("dedicated service secret must contain at least 32 bytes without surrounding whitespace")
	}
	for _, other := range otherSecrets {
		if secret == other {
			return fmt.Errorf("dedicated service secret must not reuse another credential")
		}
	}
	return nil
}
