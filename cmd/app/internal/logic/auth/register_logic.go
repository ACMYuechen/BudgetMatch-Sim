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

type RegisterLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 邮箱注册
func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RegisterLogic) Register(req *types.RegisterReq) (resp *types.RegisterResp, err error) {
	rpcResp, err := l.svcCtx.AuthClient.EmailRegister(l.ctx, &pb.EmailRegisterReq{
		Email:    req.Email,
		Password: req.Password,
		Username: req.Username,
		Code:     req.Code,
	})
	if err != nil {
		l.Logger.Errorf("failed to register: %v", err)
		return nil, errors.Internal
	}

	return &types.RegisterResp{
		Success: rpcResp.Success,
	}, nil
}
