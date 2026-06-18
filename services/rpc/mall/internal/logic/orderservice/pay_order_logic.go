package orderservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type PayOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPayOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PayOrderLogic {
	return &PayOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *PayOrderLogic) PayOrder(in *pb.PayOrderReq) (*pb.PayOrderResp, error) {
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
	if order.Status != mall_orders.OrderStatusPending {
		return nil, errors.MallInvalidOrderTransition
	}

	// 模拟支付：直接标记为已支付
	now := time.Now()
	order.Status = mall_orders.OrderStatusPaid
	order.PayType = in.PayType
	order.PayTime = &now

	if err := l.svcCtx.OrderStore.Update(l.ctx, order); err != nil {
		l.Logger.Errorf("failed to update order payment: %v", err)
		return nil, errors.Database
	}

	// 发送事件
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID: order.Id,
			UserID:  order.UserId,
			Status:  int32(mall_orders.OrderStatusPaid),
		}
		l.svcCtx.OrderEventProducer.PublishPaidAsync(event)
	}

	return &pb.PayOrderResp{
		OrderId: order.Id,
		Status:  int32(mall_orders.OrderStatusPaid),
		PayUrl:  "https://example.com/mock-pay?order_id=" + order.Id,
		Message: "mock payment success",
	}, nil
}
