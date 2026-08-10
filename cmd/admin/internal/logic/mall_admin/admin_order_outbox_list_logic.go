// Code scaffolded by goctl. No recover, Safe to edit.

package mall_admin

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdminOrderOutboxListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单Outbox事件列表
func NewAdminOrderOutboxListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminOrderOutboxListLogic {
	return &AdminOrderOutboxListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminOrderOutboxListLogic) AdminOrderOutboxList(req *types.AdminOrderOutboxListReq) (resp *types.AdminOrderOutboxListResp, err error) {
	rpcResp, err := l.svcCtx.MallOrderClient.ListOrderOutbox(l.ctx, &pb.ListOrderOutboxReq{
		Page: int32(req.Page), PageSize: int32(req.PageSize), Status: req.Status, EventType: req.EventType, AggregateId: req.AggregateId, DedupKey: req.DedupKey,
	})
	if err != nil {
		l.Logger.Errorf("failed to list order outbox events: %v", err)
		return nil, err
	}
	resp = &types.AdminOrderOutboxListResp{List: make([]types.AdminOrderOutboxEvent, 0, len(rpcResp.List)), Total: rpcResp.Total, Page: int(rpcResp.Page), PageSize: int(rpcResp.PageSize)}
	for _, event := range rpcResp.List {
		resp.List = append(resp.List, outboxEventToType(event))
	}
	return resp, nil
}
