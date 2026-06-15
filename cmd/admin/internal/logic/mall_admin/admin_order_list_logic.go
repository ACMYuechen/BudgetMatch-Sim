package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminOrderListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单列表
func NewAdminOrderListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminOrderListLogic {
	return &AdminOrderListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminOrderListLogic) AdminOrderList(req *types.AdminOrderListReq) (resp *types.AdminOrderListResp, err error) {
	rpcResp, err := l.svcCtx.MallOrderClient.ListOrders(l.ctx, &pb.ListOrdersReq{
		UserId:   req.UserId,
		Page:     int32(req.Page),
		PageSize: int32(req.PageSize),
		Status:   int32(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to list orders: %v", err)
		return nil, errors.ErrDatabase
	}

	items := make([]types.AdminOrderResp, 0, len(rpcResp.List))
	for _, o := range rpcResp.List {
		items = append(items, orderToType(o))
	}

	return &types.AdminOrderListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
