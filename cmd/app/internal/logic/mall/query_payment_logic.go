// Code scaffolded by goctl. No recover, Safe to edit.

package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	paymentpb "budgetmatch-sim/services/rpc/payment/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type QueryPaymentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 主动查询支付状态
func NewQueryPaymentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryPaymentLogic {
	return &QueryPaymentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryPaymentLogic) QueryPayment(req *types.MallQueryPaymentReq) (resp *types.MallQueryPaymentResp, err error) {
	userID, err := authenticatedUserID(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}

	order, err := loadPaymentOrder(l.ctx, l.svcCtx, req.Id, userID)
	if err != nil {
		l.Logger.Errorf("failed to load payment order: %v", err)
		return nil, err
	}

	rpcResp, err := l.svcCtx.PaymentClient.QueryPayment(l.ctx, &paymentpb.QueryPaymentReq{
		OrderId: order.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to query payment: %v", err)
		return nil, err
	}
	if rpcResp == nil || rpcResp.Payment == nil {
		l.Logger.Error("payment service returned an empty query response")
		return nil, errors.Internal
	}

	payment := rpcResp.Payment
	if payment.OrderId != order.Id || payment.UserId != userID || payment.Amount != order.PayAmount || rpcResp.Status != payment.Status {
		l.Logger.Errorf("payment data does not match order %s", order.Id)
		return nil, errors.Internal
	}

	return &types.MallQueryPaymentResp{
		Status:  rpcResp.Status,
		TradeNo: rpcResp.TradeNo,
		Payment: types.MallPaymentResp{
			Id:         payment.Id,
			OutTradeNo: payment.OutTradeNo,
			OrderId:    payment.OrderId,
			UserId:     payment.UserId,
			Amount:     payment.Amount,
			Channel:    payment.Channel,
			Status:     payment.Status,
			TradeNo:    payment.TradeNo,
			BuyerId:    payment.BuyerId,
			QrCode:     payment.QrCode,
			PaidAt:     payment.PaidAt,
			CreatedAt:  payment.CreatedAt,
			UpdatedAt:  payment.UpdatedAt,
		},
	}, nil
}
