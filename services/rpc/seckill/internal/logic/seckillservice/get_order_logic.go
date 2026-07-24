package seckillservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
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
		l.Logger.Errorf("return error: %v", errors.SeckillOrderNotFound)
		return nil, errors.SeckillOrderNotFound
	}

	return &pb.GetOrderResp{
		OrderId:     order.Id,
		ActivityId:  order.ActivityId,
		SkuId:       order.SkuId,
		UserId:      order.UserId,
		Quantity:    int32(order.Quantity),
		TotalAmount: order.TotalAmount,
		Status:      int32(order.Status),
		CreatedAt:   order.CreatedAt.UnixMilli(),
	}, nil
}
