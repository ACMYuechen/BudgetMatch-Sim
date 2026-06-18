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

type CancelOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCancelOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelOrderLogic {
	return &CancelOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CancelOrderLogic) CancelOrder(in *pb.CancelOrderReq) (*pb.CancelOrderResp, error) {
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
		return nil, errors.MallOrderCannotCancel
	}

	items, err := l.svcCtx.OrderItemStore.FindByOrderId(l.ctx, order.Id)
	if err != nil {
		l.Logger.Errorf("failed to find order items: %v", err)
		return nil, errors.Database
	}
	if len(items) == 0 {
		return nil, errors.MallOrderNotFound
	}
	item := items[0]

	err = l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		result := tx.Model(&mall_orders.MallOrders{}).
			Where("id = ? AND user_id = ? AND status = ?", order.Id, in.UserId, OrderStatusPending).
			Updates(map[string]any{
				"status":     OrderStatusCancelled,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.MallOrderCannotCancel
		}

		for _, it := range items {
			result = tx.Exec(
				"UPDATE product_skus SET stock = stock + ?, sold = sold - ?, updated_at = ? WHERE id = ?",
				it.Quantity, it.Quantity, now, it.SkuId,
			)
			if result.Error != nil {
				return result.Error
			}
		}

		return nil
	})

	if err != nil {
		if err == errors.MallOrderCannotCancel {
			return nil, errors.MallOrderCannotCancel
		}
		l.Logger.Errorf("failed to cancel order: %v", err)
		return nil, errors.Database
	}

	// send event
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID:  order.Id,
			UserID:   order.UserId,
			SkuID:    item.SkuId,
			Quantity: item.Quantity,
			Status:   int32(OrderStatusCancelled),
		}
		l.svcCtx.OrderEventProducer.PublishCancelledAsync(event)
	}

	return &pb.CancelOrderResp{Success: true}, nil
}
