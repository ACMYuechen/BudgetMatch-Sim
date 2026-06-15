package orderservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
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
		return nil, errors.ErrDatabase
	}
	if order == nil {
		return nil, errors.ErrMallOrderNotFound
	}
	if order.UserId != in.UserId {
		return nil, errors.ErrMallOrderNotFound
	}
	if order.Status != OrderStatusPending {
		return nil, errors.ErrMallOrderCannotCancel
	}

	items, err := l.svcCtx.OrderItemStore.FindByOrderId(l.ctx, order.Id)
	if err != nil {
		l.Logger.Errorf("failed to find order items: %v", err)
		return nil, errors.ErrDatabase
	}
	if len(items) == 0 {
		return nil, errors.ErrMallOrderNotFound
	}
	item := items[0]

	err = l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// rollback stock
		now := time.Now()
		result := tx.Exec(
			"UPDATE product_skus SET stock = stock + ?, sold = sold - ?, updated_at = ? WHERE id = ?",
			item.Quantity, item.Quantity, now, item.SkuId,
		)
		if result.Error != nil {
			return result.Error
		}

		// update order status
		if err := tx.Model(&mall_order_items.MallOrderItems{}).Where("id = ?", order.Id).Update("status", OrderStatusCancelled).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		l.Logger.Errorf("failed to cancel order: %v", err)
		return nil, errors.ErrDatabase
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
		if err := l.svcCtx.OrderEventProducer.PublishCancelled(l.ctx, event); err != nil {
			l.Logger.Errorf("failed to send order cancelled event: %v", err)
		}
	}

	return &pb.CancelOrderResp{Success: true}, nil
}
