// Code scaffolded by goctl. No recover, Safe to edit.

package auth

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UsernameLoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 用户名登录
func NewUsernameLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UsernameLoginLogic {
	return &UsernameLoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UsernameLoginLogic) UsernameLogin(req *types.UsernameLoginReq) (resp *types.LoginResp, err error) {
	rpcResp, err := l.svcCtx.AuthClient.UsernameLogin(l.ctx, &pb.UsernameLoginReq{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		l.Logger.Errorf("failed to login by username: %v", err)
		return nil, errors.Internal
	}

	return &types.LoginResp{
		Token:  rpcResp.Token,
		UserId: rpcResp.UserId,
		Role:   int(rpcResp.Role),
	}, nil
}
