package paymentservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"
)

type QueryPaymentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewQueryPaymentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryPaymentLogic {
	return &QueryPaymentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// QueryPayment 主动向支付宝查询交易并同步本地流水。
// 这是不依赖异步通知的兜底确认路径，本地无公网时也能确认支付结果。
func (l *QueryPaymentLogic) QueryPayment(in *pb.QueryPaymentReq) (*pb.QueryPaymentResp, error) {
	if in.OutTradeNo == "" && in.OrderId == "" {
		l.Logger.Errorf("return error: %v", errors.Invalid)
		return nil, errors.Invalid
	}

	// 定位本地流水。
	var (
		record *payments.Payments
		err    error
	)
	if in.OutTradeNo != "" {
		record, err = l.svcCtx.PaymentStore.FindByOutTradeNo(l.ctx, in.OutTradeNo)
	} else {
		record, err = l.svcCtx.PaymentStore.FindByOrderID(l.ctx, in.OrderId)
	}
	if err != nil {
		l.Logger.Errorf("find payment failed: %v", err)
		return nil, errors.Database
	}
	if record == nil {
		l.Logger.Errorf("return error: %v", errors.NotFound)
		return nil, errors.NotFound
	}

	// 已成功则直接返回本地状态，无需再查支付宝。
	if record.Status == payments.StatusSuccess {
		if err := markPaid(l.ctx, l.svcCtx, record, record.TradeNo, record.BuyerId, ""); err != nil {
			l.Logger.Errorf("mark paid failed: %v", err)
			return nil, err
		}
		return &pb.QueryPaymentResp{
			Status:  int32(record.Status),
			TradeNo: record.TradeNo,
			Payment: paymentToPb(record),
			QrCode:  record.QrCode,
		}, nil
	}

	if l.svcCtx.Alipay == nil {
		l.Logger.Error("alipay not configured")
		return nil, errors.Internal
	}

	// 向支付宝查询最新交易状态。
	res, err := l.svcCtx.Alipay.Query(l.ctx, record.OutTradeNo, "")
	if err != nil {
		l.Logger.Errorf("alipay query failed: %v", err)
		return nil, errors.Internal
	}

	if res.Paid {
		if err := markPaid(l.ctx, l.svcCtx, record, res.TradeNo, res.BuyerID, ""); err != nil {
			l.Logger.Errorf("mark paid failed: %v", err)
			return nil, err
		}
	}

	return &pb.QueryPaymentResp{
		Status:  int32(record.Status),
		TradeNo: record.TradeNo,
		Payment: paymentToPb(record),
		QrCode:  record.QrCode,
	}, nil
}
