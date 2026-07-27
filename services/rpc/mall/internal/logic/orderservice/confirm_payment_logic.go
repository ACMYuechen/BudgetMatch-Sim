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

type ConfirmPaymentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewConfirmPaymentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConfirmPaymentLogic {
	return &ConfirmPaymentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ConfirmPaymentLogic) ConfirmPayment(in *pb.ConfirmPaymentReq) (*pb.ConfirmPaymentResp, error) {
	// 1. 校验入参非空
	if in.OrderId == "" || in.UserId == "" || in.OutTradeNo == "" || in.TradeNo == "" || in.Amount <= 0 {
		return nil, errors.Invalid
	}

	// 2. 校验订单存在
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

	// 3. 校验订单状态必须是待支付或已支付
	if order.Status != mall_orders.OrderStatusPending && order.Status != mall_orders.OrderStatusPaid {
		return nil, errors.MallInvalidOrderTransition
	}

	// 4. 校验支付金额等于订单金额
	if in.Amount != order.PayAmount {
		return nil, errors.MallPaymentAmountMismatch
	}

	// 5. 已支付订单重复确认直接成功返回
	if order.Status == mall_orders.OrderStatusPaid {
		return &pb.ConfirmPaymentResp{
			Success:          true,
			Status:           int32(mall_orders.OrderStatusPaid),
			AlreadyConfirmed: true,
		}, nil
	}

	// 6. 待支付订单更新为已支付（乐观锁）
	now := time.Now()
	err = l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		ok, err := l.svcCtx.OrderStore.UpdateStatusTx(
			tx, order.Id, order.UserId, order.Status, mall_orders.OrderStatusPaid, now,
		)
		if err != nil {
			return err
		}
		if !ok {
			return errors.MallInvalidOrderTransition
		}
		// 首次转为已支付时补充支付时间
		if order.PayTime.IsZero() {
			if err := tx.Model(&mall_orders.MallOrders{}).
				Where("id = ?", order.Id).
				Update("pay_time", now).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if err == errors.MallInvalidOrderTransition {
			return nil, errors.MallInvalidOrderTransition
		}
		l.Logger.Errorf("failed to confirm payment: %v", err)
		return nil, errors.Database
	}

	// 7. 发送已支付事件
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID: order.Id,
			UserID:  order.UserId,
			Status:  int32(mall_orders.OrderStatusPaid),
		}
		l.svcCtx.OrderEventProducer.PublishPaidAsync(event)
	}

	return &pb.ConfirmPaymentResp{
		Success:          true,
		Status:           int32(mall_orders.OrderStatusPaid),
		AlreadyConfirmed: false,
	}, nil
}
