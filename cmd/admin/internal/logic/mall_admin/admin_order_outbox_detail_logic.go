// Code scaffolded by goctl. No recover, Safe to edit.

package mall_admin

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdminOrderOutboxDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单Outbox事件详情
func NewAdminOrderOutboxDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminOrderOutboxDetailLogic {
	return &AdminOrderOutboxDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminOrderOutboxDetailLogic) AdminOrderOutboxDetail(req *types.AdminOrderOutboxDetailReq) (resp *types.AdminOrderOutboxDetailResp, err error) {
	rpcResp, err := l.svcCtx.MallOrderClient.GetOrderOutbox(l.ctx, &pb.GetOrderOutboxReq{Id: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to get order outbox event: event_id=%s error=%v", req.Id, err)
		return nil, err
	}
	return &types.AdminOrderOutboxDetailResp{Event: outboxEventToType(rpcResp.Event)}, nil
}
