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
		l.Logger.Errorf("return error: %v", errors.MallOrderNotFound)
		return nil, errors.MallOrderNotFound
	}
	if order.UserId != in.UserId {
		l.Logger.Errorf("return error: %v", errors.MallOrderNotFound)
		return nil, errors.MallOrderNotFound
	}
	if order.Status != mall_orders.OrderStatusPending {
		l.Logger.Errorf("return error: %v", errors.MallOrderCannotCancel)
		return nil, errors.MallOrderCannotCancel
	}

	items, err := l.svcCtx.OrderItemStore.FindByOrderId(l.ctx, order.Id)
	if err != nil {
		l.Logger.Errorf("failed to find order items: %v", err)
		return nil, errors.Database
	}
	if len(items) == 0 {
		l.Logger.Errorf("return error: %v", errors.MallOrderNotFound)
		return nil, errors.MallOrderNotFound
	}
	item := items[0]

	if err := l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		// 更新订单状态为已取消（乐观锁校验：待支付 + 指定用户）
		ok, err := l.svcCtx.OrderStore.UpdateStatusTx(
			tx, order.Id, in.UserId,
			mall_orders.OrderStatusPending, mall_orders.OrderStatusCancelled, now,
		)
		if err != nil {
			l.Logger.Errorf("return error: %v", err)
			return err
		}
		if !ok {
			l.Logger.Errorf("return error: %v", errors.MallOrderCannotCancel)
			return errors.MallOrderCannotCancel
		}

		// 恢复各订单项对应的 SKU 库存
		for _, it := range items {
			if err := l.svcCtx.SkuStore.RestoreStockTx(tx, it.SkuId, it.Quantity, now); err != nil {
				l.Logger.Errorf("return error: %v", err)
				return err
			}
		}

		outboxEvent, err := outbox.NewOrderEvent(mq.EventTypeCancelled, now, mq.OrderEvent{
			OrderId:  order.Id,
			UserId:   order.UserId,
			SkuId:    item.SkuId,
			Quantity: item.Quantity,
			Status:   int32(mall_orders.OrderStatusCancelled),
		})
		if err != nil {
			l.Logger.Errorf("failed to build order cancelled event: order_id=%s error=%v", order.Id, err)
			return err
		}
		if err := l.svcCtx.OrderOutboxStore.InsertTx(tx, outboxEvent); err != nil {
			l.Logger.Errorf("failed to insert order cancelled outbox event: order_id=%s event_id=%s error=%v", order.Id, outboxEvent.Id, err)
			return err
		}

		return nil
	}); err != nil {
		if err == errors.MallOrderCannotCancel {
			l.Logger.Errorf("return error: %v", errors.MallOrderCannotCancel)
			return nil, errors.MallOrderCannotCancel
		}
		l.Logger.Errorf("failed to cancel order: %v", err)
		return nil, errors.Database
	}

	return &pb.CancelOrderResp{Success: true}, nil
}
