package authservicelogic

import (
	"context"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

type LoginByCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginByCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginByCodeLogic {
	return &LoginByCodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *LoginByCodeLogic) LoginByCode(in *pb.LoginByCodeReq) (*pb.LoginResp, error) {
	if in.Email == "" || in.Code == "" {
		l.Logger.Infof("invalid email or code: email=%v, code=%v", in.Email, in.Code)
		return nil, errors.InvalidEmail
	}
	// 从Redis取验证码
	key := CodeRedisKeyPrefix + in.Email
	code, err := l.svcCtx.Redis.Get(l.ctx, key).Result()
	if err == redis.Nil {
		l.Logger.Infof("verification code expired for email: %v", in.Email)
		return nil, errors.CodeExpired
	}
	if err != nil {
		l.Logger.Errorf("failed to get code from Redis for email: %v, error: %v", in.Email, err)
		return nil, errors.Database
	}
	// 校验验证码
	if code != in.Code {
		l.Logger.Infof("invalid verification code for email: %v", in.Email)
		return nil, errors.CodeInvalid
	}

	// 一次性使用，删除验证码
	l.svcCtx.Redis.Del(l.ctx, key)

	// 查询用户
	u, err := l.svcCtx.UserStore.FindByEmail(l.ctx, in.Email)
	if err != nil {
		l.Logger.Errorf("failed to get user by email: %v, error: %v", in.Email, err)
		return nil, errors.Database
	}
	if u == nil {
		l.Logger.Infof("user not found with email: %v", in.Email)
		return nil, errors.UserNotFound
	}

	// 生成 token
	token, err := auth.GenerateToken(u.Id, l.svcCtx.Config.JwtAuth.Secret, l.svcCtx.Config.JwtAuth.Expire, int(u.Role))
	if err != nil {
		l.Logger.Errorf("failed to generate token for user id: %v, error: %v", u.Id, err)
		return nil, errors.TokenGeneration
	}

	return &pb.LoginResp{
		UserId: u.Id,
		Token:  token,
		Role:   int32(u.Role),
	}, nil
}
