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
	if order.Status != mall_orders.OrderStatusPending {
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

	if err := l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		// 更新订单状态为已取消（乐观锁校验：待支付 + 指定用户）
		ok, err := l.svcCtx.OrderStore.UpdateStatusTx(
			tx, order.Id, in.UserId,
			mall_orders.OrderStatusPending, mall_orders.OrderStatusCancelled, now,
		)
		if err != nil {
			return err
		}
		if !ok {
			return errors.MallOrderCannotCancel
		}

		// 恢复各订单项对应的 SKU 库存
		for _, it := range items {
			if err := l.svcCtx.SkuStore.RestoreStockTx(tx, it.SkuId, it.Quantity, now); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		if err == errors.MallOrderCannotCancel {
			return nil, errors.MallOrderCannotCancel
		}
		l.Logger.Errorf("failed to cancel order: %v", err)
		return nil, errors.Database
	}

	// 发送事件
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID:  order.Id,
			UserID:   order.UserId,
			SkuID:    item.SkuId,
			Quantity: item.Quantity,
			Status:   int32(mall_orders.OrderStatusCancelled),
		}
		l.svcCtx.OrderEventProducer.PublishCancelledAsync(event)
	}

	return &pb.CancelOrderResp{Success: true}, nil
}
