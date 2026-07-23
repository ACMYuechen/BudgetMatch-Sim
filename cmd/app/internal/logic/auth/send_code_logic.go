// Code scaffolded by goctl. No recover, Safe to edit.

package auth

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SendCodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 发送验证码
func NewSendCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendCodeLogic {
	return &SendCodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SendCodeLogic) SendCode(req *types.SendCodeReq) (resp *types.SendCodeResp, err error) {
	rpcResp, err := l.svcCtx.AuthClient.SendCode(l.ctx, &pb.SendCodeReq{
		Email: req.Email,
	})
	if err != nil {
		l.Logger.Errorf("failed to send code: %v", err)
		return nil, err
	}

	return &types.SendCodeResp{
		Success: rpcResp.Success,
	}, nil
}
