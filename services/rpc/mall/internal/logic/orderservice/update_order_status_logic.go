package orderservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
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

	newStatus := int(in.Status)
	if !isValidOrderTransition(order.Status, newStatus) {
		return nil, errors.MallInvalidOrderTransition
	}

	now := time.Now()
	if err := l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// 乐观锁条件更新：仅当订单仍为读取时的状态、且归属同一用户时才流转，避免并发绕过状态机
		ok, err := l.svcCtx.OrderStore.UpdateStatusTx(tx, order.Id, order.UserId, order.Status, newStatus, now)
		if err != nil {
			return err
		}
		if !ok {
			return errors.MallInvalidOrderTransition
		}
		// 首次转为已支付时补充支付时间
		if newStatus == mall_orders.OrderStatusPaid && order.PayTime.IsZero() {
			if err := tx.Model(&mall_orders.MallOrders{}).Where("id = ?", order.Id).Update("pay_time", now).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		if err == errors.MallInvalidOrderTransition {
			return nil, errors.MallInvalidOrderTransition
		}
		l.Logger.Errorf("failed to update order status: %v", err)
		return nil, errors.Database
	}

	// 针对已支付状态发送事件
	if l.svcCtx.OrderEventProducer != nil && newStatus == mall_orders.OrderStatusPaid {
		event := mq.OrderEvent{
			OrderID: order.Id,
			UserID:  order.UserId,
			Status:  int32(mall_orders.OrderStatusPaid),
		}
		l.svcCtx.OrderEventProducer.PublishPaidAsync(event)
	}

	return &pb.UpdateOrderStatusResp{Success: true}, nil
}

func isValidOrderTransition(current, next int) bool {
	switch current {
	case mall_orders.OrderStatusPending:
		return next == mall_orders.OrderStatusPaid || next == mall_orders.OrderStatusCancelled
	case mall_orders.OrderStatusPaid:
		return next == mall_orders.OrderStatusShipped || next == mall_orders.OrderStatusCancelled
	case mall_orders.OrderStatusShipped:
		return next == mall_orders.OrderStatusCompleted || next == mall_orders.OrderStatusRefunding
	case mall_orders.OrderStatusRefunding:
		return next == mall_orders.OrderStatusRefunded
	case mall_orders.OrderStatusCompleted, mall_orders.OrderStatusCancelled, mall_orders.OrderStatusRefunded:
		return false
	default:
		return false
	}
}
