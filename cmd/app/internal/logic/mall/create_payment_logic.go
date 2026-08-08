// Code scaffolded by goctl. No recover, Safe to edit.

package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	mallpb "budgetmatch-sim/services/rpc/mall/pb"
	paymentpb "budgetmatch-sim/services/rpc/payment/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreatePaymentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 发起支付宝扫码支付
func NewCreatePaymentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatePaymentLogic {
	return &CreatePaymentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreatePaymentLogic) CreatePayment(req *types.MallCreatePaymentReq) (resp *types.MallCreatePaymentResp, err error) {
	userId, err := authenticatedUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}

	order, err := loadPaymentOrder(l.ctx, l.svcCtx, req.Id, userId)
	if err != nil {
		l.Logger.Errorf("failed to load payment order: %v", err)
		return nil, err
	}
	if order.Status != mallpb.OrderStatus_ORDER_STATUS_PENDING {
		l.Logger.Errorf("order %s is not pending payment, status=%d", order.Id, order.Status)
		return nil, errors.MallInvalidOrderTransition
	}

	rpcResp, err := l.svcCtx.PaymentClient.CreatePayment(l.ctx, &paymentpb.CreatePaymentReq{
		OrderId: order.Id,
		UserId:  userId,
		Amount:  order.PayAmount,
	})
	if err != nil {
		l.Logger.Errorf("failed to create payment: %v", err)
		return nil, err
	}
	if rpcResp == nil {
		l.Logger.Error("payment service returned an empty create response")
		return nil, errors.Internal
	}

	return &types.MallCreatePaymentResp{
		OutTradeNo: rpcResp.OutTradeNo,
		QrCode:     rpcResp.QrCode,
		Status:     rpcResp.Status,
	}, nil
}
