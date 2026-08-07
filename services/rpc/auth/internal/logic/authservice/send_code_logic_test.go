package authservicelogic

import (
	"context"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/config"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupSendCodeTestRedis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	s := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: s.Addr()})
	return s, r
}

// TestSendCode_InvalidEmail 测试无效邮箱格式场景
// 验证使用无效邮箱格式发送验证码时，返回 InvalidEmail 错误
func TestSendCode_InvalidEmail(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
	}

	l := NewSendCodeLogic(context.Background(), svcCtx)
	_, err := l.SendCode(&pb.SendCodeReq{
		Email: "invalid-email",
	})

	if err != errors.InvalidEmail {
		t.Fatalf("期望 InvalidEmail，但得到: %v", err)
	}
}

// TestSendCode_EmptyEmail 测试空邮箱场景
// 验证使用空邮箱发送验证码时，返回 InvalidEmail 错误
func TestSendCode_EmptyEmail(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
	}

	l := NewSendCodeLogic(context.Background(), svcCtx)
	_, err := l.SendCode(&pb.SendCodeReq{
		Email: "",
	})

	if err != errors.InvalidEmail {
		t.Fatalf("期望 InvalidEmail，但得到: %v", err)
	}
}

// TestSendCode_RateLimitExceeded 测试60秒内重复发送验证码被限流场景
// 验证在60秒内对同一邮箱重复发送验证码时，返回 TooManyRequests 错误
func TestSendCode_RateLimitExceeded(t *testing.T) {
	_, redisClient := setupSendCodeTestRedis(t)

	// 先设置限流标记（模拟已发送过验证码）
	rateLimitKey := RateLimitRedisKeyPrefix + "user@example.com"
	redisClient.SetNX(context.Background(), rateLimitKey, "1", 60*time.Second).Result()

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
		Redis: redisClient,
	}

	l := NewSendCodeLogic(context.Background(), svcCtx)
	_, err := l.SendCode(&pb.SendCodeReq{
		Email: "user@example.com",
	})

	if err != errors.TooManyRequests {
		t.Fatalf("期望 TooManyRequests，但得到: %v", err)
	}
}

// TestSendCode_RateLimitWindow 验证限流窗口常量是否正确
// 验证 RateLimitWindow 常量值为 60 秒
func TestSendCode_RateLimitWindow(t *testing.T) {
	if RateLimitWindow != 60 {
		t.Fatalf("RateLimitWindow 应该为 60 秒，但实际为 %d", RateLimitWindow)
	}
}

// TestSendCode_CodeExpireTime 验证验证码过期时间常量是否正确
// 验证 CodeExpireTime 常量值为 300 秒（5分钟）
func TestSendCode_CodeExpireTime(t *testing.T) {
	if CodeExpireTime != 300 {
		t.Fatalf("CodeExpireTime 应该为 300 秒，但实际为 %d", CodeExpireTime)
	}
}
