package authservicelogic

import (
	"context"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UsernameLoginLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUsernameLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UsernameLoginLogic {
	return &UsernameLoginLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UsernameLoginLogic) UsernameLogin(in *pb.UsernameLoginReq) (*pb.LoginResp, error) {
	// 用户名是否存在
	u, err := l.svcCtx.UserStore.FindByUsername(l.ctx, in.Username)
	if err != nil {
		l.Logger.Errorf("failed to find user by username: %v, error: %v", in.Username, err)
		return nil, errors.Database
	}
	if u == nil {
		l.Logger.Infof("user not found with username: %v", in.Username)
		return nil, errors.UserNotFound
	}

	// 验证密码
	if err := auth.ComparePassword(u.Password, in.Password); err != nil {
		l.Logger.Infof("invalid password for username: %v", in.Username)
		return nil, errors.InvalidPassword
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
