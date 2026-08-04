package auth

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CheckUsernameLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCheckUsernameLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckUsernameLogic {
	return &CheckUsernameLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CheckUsernameLogic) CheckUsername(req *types.CheckUsernameReq) (*types.CheckUsernameResp, error) {
	rpcResp, err := l.svcCtx.AuthClient.CheckUsername(l.ctx, &pb.CheckUsernameReq{
		Username: req.Username,
	})
	if err != nil {
		l.Logger.Errorf("failed to check username: %v", err)
		return nil, err
	}

	return &types.CheckUsernameResp{
		Exists: rpcResp.Exists,
	}, nil
}
