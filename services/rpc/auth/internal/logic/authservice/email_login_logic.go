package authservicelogic

import (
	"context"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type EmailLoginLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewEmailLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EmailLoginLogic {
	return &EmailLoginLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *EmailLoginLogic) EmailLogin(in *pb.EmailLoginReq) (*pb.LoginResp, error) {
	// 用户邮箱是否存在
	u, err := l.svcCtx.UserStore.FindByEmail(l.ctx, in.Email)
	if err != nil {
		l.Logger.Errorf("failed to find user by email: %v, error: %v", in.Email, err)
		return nil, errors.ErrDatabase
	}
	if u == nil {
		l.Logger.Infof("user not found with email: %v", in.Email)
		return nil, errors.ErrUserNotFound
	}

	// 验证密码
	if err := auth.ComparePassword(u.Password, in.Password); err != nil {
		l.Logger.Infof("invalid password for email: %v", in.Email)
		return nil, errors.ErrInvalidPassword
	}

	// 生成 token
	token, err := auth.GenerateToken(u.Id, l.svcCtx.Config.JwtAuth.Secret, l.svcCtx.Config.JwtAuth.Expire, int(u.Role))
	if err != nil {
		l.Logger.Errorf("failed to generate token for user id: %v, error: %v", u.Id, err)
		return nil, errors.ErrTokenGeneration
	}

	return &pb.LoginResp{
		UserId: u.Id,
		Token:  token,
		Role:   int32(u.Role),
	}, nil
}
