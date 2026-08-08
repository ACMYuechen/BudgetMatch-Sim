package orderservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/outbox"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
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
		l.Logger.Errorf("return error: %v", errors.MallOrderNotFound)
		return nil, errors.MallOrderNotFound
	}
	if in.UserId != "" && order.UserId != in.UserId {
		l.Logger.Errorf("return error: %v", errors.MallOrderNotFound)
		return nil, errors.MallOrderNotFound
	}

	newStatus := int(in.Status)
	if !isValidOrderTransition(order.Status, newStatus) {
		l.Logger.Errorf("return error: %v", errors.MallInvalidOrderTransition)
		return nil, errors.MallInvalidOrderTransition
	}

	var cancellationItems []mall_order_items.MallOrderItems
	if newStatus == mall_orders.OrderStatusCancelled {
		cancellationItems, err = l.svcCtx.OrderItemStore.FindByOrderId(l.ctx, order.Id)
		if err != nil {
			l.Logger.Errorf("failed to find order items before cancellation: order_id=%s error=%v", order.Id, err)
			return nil, errors.Database
		}
		if len(cancellationItems) == 0 {
			return nil, errors.MallOrderNotFound
		}
	}

	now := time.Now()
	if err := l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// 乐观锁条件更新：仅当订单仍为读取时的状态、且归属同一用户时才流转，避免并发绕过状态机
		ok, err := l.svcCtx.OrderStore.UpdateStatusTx(tx, order.Id, order.UserId, order.Status, newStatus, now)
		if err != nil {
			l.Logger.Errorf("return error: %v", err)
			return err
		}
		if !ok {
			l.Logger.Errorf("return error: %v", errors.MallInvalidOrderTransition)
			return errors.MallInvalidOrderTransition
		}
		// 首次转为已支付时补充支付时间
		if newStatus == mall_orders.OrderStatusPaid && order.PayTime.IsZero() {
			if err := tx.Model(&mall_orders.MallOrders{}).Where("id = ?", order.Id).Update("pay_time", now).Error; err != nil {
				l.Logger.Errorf("return error: %v", err)
				return err
			}
		}
		if newStatus == mall_orders.OrderStatusCancelled {
			for _, item := range cancellationItems {
				if err := l.svcCtx.SkuStore.RestoreStockTx(tx, item.SkuId, item.Quantity, now); err != nil {
					l.Logger.Errorf("failed to restore stock during admin cancellation: order_id=%s sku_id=%s error=%v", order.Id, item.SkuId, err)
					return err
				}
			}
		}

		if newStatus == mall_orders.OrderStatusPaid || newStatus == mall_orders.OrderStatusCancelled {
			eventType := mq.EventTypePaid
			event := mq.OrderEvent{OrderId: order.Id, UserId: order.UserId, Status: int32(newStatus)}
			if newStatus == mall_orders.OrderStatusCancelled {
				eventType = mq.EventTypeCancelled
				event.SkuId = cancellationItems[0].SkuId
				event.Quantity = cancellationItems[0].Quantity
			}
			outboxEvent, err := outbox.NewOrderEvent(eventType, now, event)
			if err != nil {
				l.Logger.Errorf("failed to build order status event: order_id=%s event_type=%s error=%v", order.Id, eventType, err)
				return err
			}
			if err := l.svcCtx.OrderOutboxStore.InsertTx(tx, outboxEvent); err != nil {
				l.Logger.Errorf("failed to insert order status outbox event: order_id=%s event_id=%s error=%v", order.Id, outboxEvent.Id, err)
				return err
			}
		}
		return nil
	}); err != nil {
		if err == errors.MallInvalidOrderTransition {
			l.Logger.Errorf("return error: %v", errors.MallInvalidOrderTransition)
			return nil, errors.MallInvalidOrderTransition
		}
		l.Logger.Errorf("failed to update order status: %v", err)
		return nil, errors.Database
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
