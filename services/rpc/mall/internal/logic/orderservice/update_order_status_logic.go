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

type UpdateOrderStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateOrderStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateOrderStatusLogic {
	return &UpdateOrderStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateOrderStatusLogic) UpdateOrderStatus(in *pb.UpdateOrderStatusReq) (*pb.UpdateOrderStatusResp, error) {
	order, err := l.svcCtx.OrderStore.FindOne(l.ctx, in.OrderId)
	if err != nil {
		l.Logger.Errorf("failed to find order: %v", err)
		return nil, errors.Database
	}
	if order == nil {
		return nil, errors.MallOrderNotFound
	}
	if in.UserId != "" && order.UserId != in.UserId {
		return nil, errors.MallOrderNotFound
	}

	newStatus := int64(in.Status)
	if !isValidOrderTransition(order.Status, newStatus) {
		return nil, errors.MallInvalidOrderTransition
	}

	order.Status = newStatus
	if newStatus == OrderStatusPaid && order.PayTime == nil {
		now := time.Now()
		order.PayTime = &now
	}

	if err := l.svcCtx.OrderStore.Update(l.ctx, order); err != nil {
		l.Logger.Errorf("failed to update order status: %v", err)
		return nil, errors.Database
	}

	// send event for paid status
	if l.svcCtx.OrderEventProducer != nil && newStatus == OrderStatusPaid {
		event := mq.OrderEvent{
			OrderID: order.Id,
			UserID:  order.UserId,
			Status:  int32(OrderStatusPaid),
		}
		if err := l.svcCtx.OrderEventProducer.PublishPaid(l.ctx, event); err != nil {
			l.Logger.Errorf("failed to send order paid event: %v", err)
		}
	}

	return &pb.UpdateOrderStatusResp{Success: true}, nil
}

func isValidOrderTransition(current, next int64) bool {
	switch current {
	case OrderStatusPending:
		return next == OrderStatusPaid || next == OrderStatusCancelled
	case OrderStatusPaid:
		return next == OrderStatusShipped || next == OrderStatusCancelled
	case OrderStatusShipped:
		return next == OrderStatusCompleted || next == OrderStatusRefunding
	case OrderStatusRefunding:
		return next == OrderStatusRefunded
	case OrderStatusCompleted, OrderStatusCancelled, OrderStatusRefunded:
		return false
	default:
		return false
	}
}
