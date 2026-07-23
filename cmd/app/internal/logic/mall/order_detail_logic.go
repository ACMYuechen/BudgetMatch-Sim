// Code scaffolded by goctl. No recover, Safe to edit.

package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type OrderDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单详情
func NewOrderDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OrderDetailLogic {
	return &OrderDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OrderDetailLogic) OrderDetail(req *types.MallOrderDetailReq) (resp *types.MallOrderDetailResp, err error) {
	userID := l.ctx.Value("user_id")
	if userID == nil {
		l.Logger.Errorf("return error: %v", errors.Unauthorized)
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.MallOrderClient.GetOrder(l.ctx, &pb.GetOrderReq{
		OrderId: req.Id,
		UserId:  userID.(string),
	})
	if err != nil {
		l.Logger.Errorf("failed to get order: %v", err)
		return nil, err
	}

	return &types.MallOrderDetailResp{
		Order: orderToType(rpcResp.Order),
	}, nil
}
