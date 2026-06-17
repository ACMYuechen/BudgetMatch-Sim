// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ActivityOnlineLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 上线活动
func NewActivityOnlineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityOnlineLogic {
	return &ActivityOnlineLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityOnlineLogic) ActivityOnline(req *types.ActivityOnlineReq) (resp *types.ActivityOnlineResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.OnlineActivity(l.ctx, &pb.OnlineActivityReq{
		Id: req.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to online activity: %v", err)
		return nil, errors.Database
	}

	return &types.ActivityOnlineResp{
		Success: rpcResp.Success,
	}, nil
}
