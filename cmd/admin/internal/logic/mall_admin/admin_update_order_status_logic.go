package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminUpdateOrderStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新订单状态
func NewAdminUpdateOrderStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminUpdateOrderStatusLogic {
	return &AdminUpdateOrderStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminUpdateOrderStatusLogic) AdminUpdateOrderStatus(req *types.AdminUpdateOrderStatusReq) (resp *types.AdminUpdateOrderStatusResp, err error) {
	_, err = l.svcCtx.MallOrderClient.UpdateOrderStatus(l.ctx, &pb.UpdateOrderStatusReq{
		OrderId: req.Id,
		Status:  req.Status,
	})
	if err != nil {
		l.Logger.Errorf("failed to update order status: %v", err)
		return nil, errors.Internal
	}

	return &types.AdminUpdateOrderStatusResp{Success: true}, nil
}
