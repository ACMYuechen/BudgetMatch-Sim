package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const testSecret = "test-secret-key-for-unit-testing"

func TestGenerateTokenAndValidate(t *testing.T) {
	userId := "user-123"
	role := 1
	expire := int64(3600) // 1小时

	// 生成 token
	token, err := GenerateToken(userId, testSecret, expire, role)
	if err != nil {
		t.Fatalf("生成 token 失败: %v", err)
	}
	if token == "" {
		t.Fatal("token 为空")
	}
	t.Logf("生成 token: %s...", token[:20])

	// 验证 token
	parsed, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("验证 token 失败: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("token 无效")
	}

	// 提取 user_id
	extractedUserId, err := GetUserIdFromToken(token, testSecret)
	if err != nil {
		t.Fatalf("提取 user_id 失败: %v", err)
	}
	if extractedUserId != userId {
		t.Fatalf("user_id 不匹配: got %s, want %s", extractedUserId, userId)
	}

	// 提取 role
	extractedRole, err := GetUserRoleFromToken(token, testSecret)
	if err != nil {
		t.Fatalf("提取 role 失败: %v", err)
	}
	if extractedRole != role {
		t.Fatalf("role 不匹配: got %d, want %d", extractedRole, role)
	}
}

func TestValidateToken_InvalidSecret(t *testing.T) {
	token, _ := GenerateToken("user-123", testSecret, 3600, 1)

	// 用错误的 secret 验证
	_, err := ValidateToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("用错误 secret 验证应该失败")
	}
}

func TestIsTokenExpired(t *testing.T) {
	// 生成一个已过期 token（过期时间为 -1 秒）
	token, err := GenerateToken("user-123", testSecret, -1, 1)
	if err != nil {
		t.Fatalf("生成 token 失败: %v", err)
	}

	// 等待一小段时间确保过期
	time.Sleep(100 * time.Millisecond)

	expired, err := IsTokenExpired(token, testSecret)
	if err != nil {
		t.Fatalf("检查过期失败: %v", err)
	}
	if !expired {
		t.Fatal("token 应该已过期")
	}
}

func TestHashPasswordAndCompare(t *testing.T) {
	password := "my-secure-password-123"

	// 哈希密码
	hashed, err := HashPassword(password)
	if err != nil {
		t.Fatalf("哈希密码失败: %v", err)
	}
	if hashed == "" {
		t.Fatal("哈希结果为空")
	}
	if hashed == password {
		t.Fatal("哈希结果不应与原文相同")
	}
	t.Logf("哈希结果: %s...", hashed[:20])

	// 正确密码验证
	if err := ComparePassword(hashed, password); err != nil {
		t.Fatalf("正确密码验证失败: %v", err)
	}

	// 错误密码验证
	if err := ComparePassword(hashed, "wrong-password"); err == nil {
		t.Fatal("错误密码验证应该失败")
	}
}

func TestGetUserIdFromToken_Invalid(t *testing.T) {
	// 无效 token
	_, err := GetUserIdFromToken("invalid-token", testSecret)
	if err == nil {
		t.Fatal("无效 token 应该返回错误")
	}
}

func TestTokenClaims(t *testing.T) {
	userId := "test-user-456"
	role := 2
	expire := int64(7200)

	token, _ := GenerateToken(userId, testSecret, expire, role)

	parsed, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("验证失败: %v", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims 类型转换失败")
	}

	// 验证 exp 存在且合理
	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatal("exp claim 不存在或类型错误")
	}

	expectedExp := time.Now().Unix() + expire
	if exp < float64(expectedExp-10) || exp > float64(expectedExp+10) {
		t.Fatalf("exp 时间不合理: %v, 期望在 %v 附近", exp, expectedExp)
	}
}
