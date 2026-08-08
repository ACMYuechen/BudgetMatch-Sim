// Code scaffolded by goctl. No recover, Safe to edit.

package mall_admin

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdminOrderOutboxStatsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单Outbox状态统计
func NewAdminOrderOutboxStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminOrderOutboxStatsLogic {
	return &AdminOrderOutboxStatsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminOrderOutboxStatsLogic) AdminOrderOutboxStats() (resp *types.AdminOrderOutboxStatsResp, err error) {
	rpcResp, err := l.svcCtx.MallOrderClient.GetOrderOutboxStats(l.ctx, &pb.GetOrderOutboxStatsReq{})
	if err != nil {
		l.Logger.Errorf("failed to get order outbox stats: %v", err)
		return nil, err
	}
	resp = &types.AdminOrderOutboxStatsResp{Counts: make([]types.AdminOrderOutboxStatusCount, 0, len(rpcResp.Counts)), OldestPendingAt: rpcResp.OldestPendingAt}
	for _, count := range rpcResp.Counts {
		resp.Counts = append(resp.Counts, types.AdminOrderOutboxStatusCount{Status: count.Status, EventType: count.EventType, Count: count.Count})
	}
	return resp, nil
}
