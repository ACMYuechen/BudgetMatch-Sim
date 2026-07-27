// Code scaffolded by goctl. No recover, Safe to edit.

package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type OrderListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单列表
func NewOrderListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OrderListLogic {
	return &OrderListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OrderListLogic) OrderList(req *types.MallOrderListReq) (resp *types.MallOrderListResp, err error) {
	userID, err := authenticatedUserID(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}

	rpcResp, err := l.svcCtx.MallOrderClient.ListOrders(l.ctx, &pb.ListOrdersReq{
		UserId:   userID,
		Page:     int32(req.Page),
		PageSize: int32(req.PageSize),
		Status:   pb.OrderStatus(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to list orders: %v", err)
		return nil, err
	}

	items := make([]types.MallOrderResp, 0, len(rpcResp.List))
	for _, o := range rpcResp.List {
		items = append(items, orderToType(o))
	}

	return &types.MallOrderListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
