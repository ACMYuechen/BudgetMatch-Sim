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

type ActivityPreheatLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 预热活动
func NewActivityPreheatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityPreheatLogic {
	return &ActivityPreheatLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityPreheatLogic) ActivityPreheat(req *types.ActivityPreheatReq) (resp *types.ActivityPreheatResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.PreheatActivity(l.ctx, &pb.PreheatActivityReq{
		Id: req.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to preheat activity: %v", err)
		return nil, errors.Database
	}

	return &types.ActivityPreheatResp{
		Success: rpcResp.Success,
	}, nil
}
