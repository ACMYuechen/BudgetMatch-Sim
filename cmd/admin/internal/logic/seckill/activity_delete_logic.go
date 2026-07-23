// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ActivityDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 删除活动
func NewActivityDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityDeleteLogic {
	return &ActivityDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityDeleteLogic) ActivityDelete(req *types.ActivityDeleteReq) (resp *types.ActivityDeleteResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.DeleteActivity(l.ctx, &pb.DeleteActivityReq{
		Id: req.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to delete activity: %v", err)
		return nil, err
	}

	return &types.ActivityDeleteResp{
		Success: rpcResp.Success,
	}, nil
}
