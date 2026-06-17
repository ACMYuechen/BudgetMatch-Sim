package orderservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type GetOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOrderLogic {
	return &GetOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetOrderLogic) GetOrder(in *pb.GetOrderReq) (*pb.GetOrderResp, error) {
	order, err := l.svcCtx.OrderStore.FindOne(l.ctx, in.OrderId)
	if err != nil {
		l.Logger.Errorf("failed to find order: %v", err)
		return nil, errors.Database
	}
	if order == nil {
		return nil, errors.MallOrderNotFound
	}
	if order.UserId != in.UserId {
		return nil, errors.MallOrderNotFound
	}

	items, err := l.svcCtx.OrderItemStore.FindByOrderId(l.ctx, order.Id)
	if err != nil {
		l.Logger.Errorf("failed to find order items: %v", err)
		return nil, errors.Database
	}

	return &pb.GetOrderResp{Order: orderToPb(order, items)}, nil
}
