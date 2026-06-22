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

type EmailLoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 邮箱登录
func NewEmailLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EmailLoginLogic {
	return &EmailLoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *EmailLoginLogic) EmailLogin(req *types.EmailLoginReq) (resp *types.LoginResp, err error) {
	rpcResp, err := l.svcCtx.AuthClient.EmailLogin(l.ctx, &pb.EmailLoginReq{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		l.Logger.Errorf("failed to login by email: %v", err)
		return nil, errors.Internal
	}

	return &types.LoginResp{
		Token:  rpcResp.Token,
		UserId: rpcResp.UserId,
		Role:   rpcResp.Role,
	}, nil
}
