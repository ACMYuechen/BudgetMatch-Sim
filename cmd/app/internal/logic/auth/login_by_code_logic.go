// Code scaffolded by goctl. No recover, Safe to edit.

package auth

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginByCodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 验证码登录
func NewLoginByCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginByCodeLogic {
	return &LoginByCodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginByCodeLogic) LoginByCode(req *types.LoginByCodeReq) (resp *types.LoginResp, err error) {
	rpcResp, err := l.svcCtx.AuthClient.LoginByCode(l.ctx, &pb.LoginByCodeReq{
		Email: req.Email,
		Code:  req.Code,
	})
	if err != nil {
		l.Logger.Errorf("failed to login by code: %v", err)
		return nil, err
	}

	return &types.LoginResp{
		Token:  rpcResp.Token,
		UserId: rpcResp.UserId,
		Role:   int(rpcResp.Role),
	}, nil
}
