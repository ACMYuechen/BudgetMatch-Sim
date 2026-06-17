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

type ActivityOfflineLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 下线活动
func NewActivityOfflineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityOfflineLogic {
	return &ActivityOfflineLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityOfflineLogic) ActivityOffline(req *types.ActivityOfflineReq) (resp *types.ActivityOfflineResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.OfflineActivity(l.ctx, &pb.OfflineActivityReq{
		Id: req.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to offline activity: %v", err)
		return nil, errors.Database
	}

	return &types.ActivityOfflineResp{
		Success: rpcResp.Success,
	}, nil
}
