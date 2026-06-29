package orderservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
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
	if order.Status != mall_orders.OrderStatusPending {
		return nil, errors.MallInvalidOrderTransition
	}

	// 模拟支付：以乐观锁条件更新（WHERE status=pending），避免并发重复支付与重复发事件
	now := time.Now()
	ok, err := l.svcCtx.OrderStore.MarkPaidTx(l.svcCtx.DB, order.Id, in.UserId, mall_orders.OrderStatusPending, in.PayType, now)
	if err != nil {
		l.Logger.Errorf("failed to update order payment: %v", err)
		return nil, errors.Database
	}
	if !ok {
		// 订单已被并发请求处理或状态已变更
		return nil, errors.MallInvalidOrderTransition
	}

	// 发送事件（仅本次真正完成状态流转时发送一次）
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID: order.Id,
			UserID:  order.UserId,
			Status:  int32(mall_orders.OrderStatusPaid),
		}
		l.svcCtx.OrderEventProducer.PublishPaidAsync(event)
	}

	return &pb.PayOrderResp{
		OrderId: order.Id,
		Status:  int32(mall_orders.OrderStatusPaid),
		PayUrl:  "https://example.com/mock-pay?order_id=" + order.Id,
		Message: "mock payment success",
	}, nil
}
