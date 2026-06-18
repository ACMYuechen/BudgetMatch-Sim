package orderservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
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
	if order.Status != OrderStatusPending {
		return nil, errors.MallInvalidOrderTransition
	}

	// mock payment: directly mark as paid
	now := time.Now()
	order.Status = OrderStatusPaid
	order.PayType = in.PayType
	order.PayTime = &now

	if err := l.svcCtx.OrderStore.Update(l.ctx, order); err != nil {
		l.Logger.Errorf("failed to update order payment: %v", err)
		return nil, errors.Database
	}

	// send event
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID: order.Id,
			UserID:  order.UserId,
			Status:  int32(OrderStatusPaid),
		}
		l.svcCtx.OrderEventProducer.PublishPaidAsync(event)
	}

	return &pb.PayOrderResp{
		OrderId: order.Id,
		Status:  int32(OrderStatusPaid),
		PayUrl:  "https://example.com/mock-pay?order_id=" + order.Id,
		Message: "mock payment success",
	}, nil
}
