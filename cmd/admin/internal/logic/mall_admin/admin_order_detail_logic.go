package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminOrderDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单详情
func NewAdminOrderDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminOrderDetailLogic {
	return &AdminOrderDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminOrderDetailLogic) AdminOrderDetail(req *types.AdminOrderDetailReq) (resp *types.AdminOrderDetailResp, err error) {
	rpcResp, err := l.svcCtx.MallOrderClient.GetOrder(l.ctx, &pb.GetOrderReq{OrderId: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to get order: %v", err)
		return nil, errors.ErrMallOrderNotFound
	}
	if rpcResp.Order == nil {
		return nil, errors.ErrMallOrderNotFound
	}

	return &types.AdminOrderDetailResp{Order: orderToType(rpcResp.Order)}, nil
}
