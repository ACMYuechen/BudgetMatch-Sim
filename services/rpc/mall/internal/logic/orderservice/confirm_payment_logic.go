package orderservicelogic

import (
	"context"
	"time"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
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
	if in == nil || in.OrderId == "" || in.UserId == "" || in.OutTradeNo == "" || in.TradeNo == "" || in.Amount <= 0 {
		l.Logger.Error("invalid confirm payment request")
		return nil, errors.Invalid
	}

	var (
		order               *mall_orders.MallOrders
		wasAlreadyConfirmed bool
	)
	err := l.svcCtx.DB.WithContext(l.ctx).Transaction(func(tx *gorm.DB) error {
		// 锁住订单行，使同一订单的并发确认串行处理。
		var err error
		order, err = l.svcCtx.OrderStore.FindOneForUpdateTx(tx, in.OrderId)
		if err != nil {
			l.Logger.Errorf("failed to find order for update: order_id=%s error=%v", in.OrderId, err)
			return errors.Internal
		}
		if order == nil {
			l.Logger.Errorf("order not found: order_id=%s", in.OrderId)
			return errors.MallOrderNotFound
		}
		if order.UserId != in.UserId {
			l.Logger.Errorf("payment user mismatch: order_id=%s order_user_id=%s request_user_id=%s", order.Id, order.UserId, in.UserId)
			return errors.MallOrderNotFound
		}
		if order.PayAmount != in.Amount {
			l.Logger.Errorf("payment amount mismatch: order_id=%s order_amount=%d request_amount=%d", order.Id, order.PayAmount, in.Amount)
			return errors.MallPaymentAmountMismatch
		}

		// 订单表保存最终确认使用的交易号，作为幂等判断依据。
		if sameConfirmedPayment(order, in) {
			wasAlreadyConfirmed = true
			return nil
		}

		if order.OutTradeNo != "" || order.TradeNo != "" {
			l.Logger.Errorf("payment confirmation conflict: order_id=%s confirmed_out_trade_no=%s confirmed_trade_no=%s request_out_trade_no=%s request_trade_no=%s", order.Id, order.OutTradeNo, order.TradeNo, in.OutTradeNo, in.TradeNo)
			return errors.MallPaymentConfirmationConflict
		}
		if order.Status != mall_orders.OrderStatusPending {
			l.Logger.Errorf("invalid order status for payment confirmation: order_id=%s status=%d", order.Id, order.Status)
			return errors.MallInvalidOrderTransition
		}

		now := time.Now()
		ok, err := l.svcCtx.OrderStore.ConfirmPaymentTx(tx, &mall_orders.ConfirmPaymentTxReq{
			Id:         in.OrderId,
			UserId:     in.UserId,
			OutTradeNo: in.OutTradeNo,
			TradeNo:    in.TradeNo,
			Now:        now,
		})
		if err != nil {
			l.Logger.Errorf("failed to update order payment: order_id=%s error=%v", order.Id, err)
			return errors.Internal
		}
		if !ok {
			l.Logger.Errorf("order payment update affected no rows: order_id=%s expected_status=%d", order.Id, mall_orders.OrderStatusPending)
			return errors.MallInvalidOrderTransition
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if wasAlreadyConfirmed {
		l.Logger.Infof("payment already confirmed: order_id=%s out_trade_no=%s trade_no=%s status=%d", order.Id, in.OutTradeNo, in.TradeNo, order.Status)
		return &pb.ConfirmPaymentResp{
			Success:             true,
			Status:              int32(order.Status),
			WasAlreadyConfirmed: true,
		}, nil
	}

	// 只有首次完成订单状态流转时才发送事件。
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID: order.Id,
			UserID:  order.UserId,
			Status:  int32(mall_orders.OrderStatusPaid),
		}
		l.svcCtx.OrderEventProducer.PublishPaidAsync(event)
	}

	l.Logger.Infof("payment confirmed: order_id=%s user_id=%s amount=%d out_trade_no=%s trade_no=%s", order.Id, order.UserId, in.Amount, in.OutTradeNo, in.TradeNo)

	return &pb.ConfirmPaymentResp{
		Success:             true,
		Status:              int32(mall_orders.OrderStatusPaid),
		WasAlreadyConfirmed: false,
	}, nil
}

func sameConfirmedPayment(order *mall_orders.MallOrders, in *pb.ConfirmPaymentReq) bool {
	return order.OutTradeNo != "" &&
		order.TradeNo != "" &&
		order.OutTradeNo == in.OutTradeNo &&
		order.TradeNo == in.TradeNo
}
