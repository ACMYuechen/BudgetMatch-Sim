package authservicelogic

import (
	"context"
	"regexp"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/rand"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

var sendCodeEmailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

type SendCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendCodeLogic {
	return &SendCodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SendCodeLogic) SendCode(in *pb.SendCodeReq) (*pb.SendCodeResp, error) {
	email := in.Email
	if email == "" || !sendCodeEmailRegex.MatchString(email) {
		l.Logger.Errorf("return error: %v", errors.InvalidEmail)
		return nil, errors.InvalidEmail
	}

	// 限流检查：原子性 SETNX，避免竞态条件
	rateLimitKey := RateLimitRedisKeyPrefix + email
	ok, err := l.svcCtx.Redis.SetNX(l.ctx, rateLimitKey, "1", RateLimitWindow*time.Second).Result()
	if err != nil {
		l.Logger.Errorf("failed to check rate limit for email: %v, error: %v", email, err)
		return nil, errors.Database
	}
	if !ok {
		l.Logger.Errorf("return error: %v", errors.TooManyRequests)
		return nil, errors.TooManyRequests
	}

	// 生成验证码
	code := rand.GenCode()

	// 存入Redis 5分钟
	key := CodeRedisKeyPrefix + email
	err = l.svcCtx.Redis.Set(l.ctx, key, code, CodeExpireTime*time.Second).Err()
	if err != nil {
		l.Logger.Errorf("failed to set code for email: %v, error: %v", email, err)
		return nil, errors.Database
	}

	// 发送邮件
	err = auth.SendEmail(l.svcCtx.Config.JwtAuth, email, code)
	if err != nil {
		l.Logger.Errorf("failed to send email to: %v, error: %v", email, err)
		// 邮件发送失败，回滚已写入的验证码
		_ = l.svcCtx.Redis.Del(l.ctx, key).Err()
		l.Logger.Errorf("return error: %v", errors.EmailSendFailed)
		return nil, errors.EmailSendFailed
	}

	return &pb.SendCodeResp{
		Success: true,
	}, nil
}
