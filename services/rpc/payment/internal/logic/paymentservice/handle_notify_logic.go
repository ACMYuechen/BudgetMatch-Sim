package paymentservicelogic

import (
	infraalipay "budgetmatch-sim/infra/alipay"
	"context"
	"encoding/json"

	"github.com/smartwalle/alipay/v3"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"
)

type HandleNotifyLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewHandleNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HandleNotifyLogic {
	return &HandleNotifyLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// HandleNotify 处理支付宝异步通知：验签 → 幂等确认 → 同步流水。
// REST 网关只负责把原始表单参数透传进来，验签在此完成（密钥不出 payment-rpc）。
func (l *HandleNotifyLogic) HandleNotify(in *pb.HandleNotifyReq) (*pb.HandleNotifyResp, error) {
	if l.svcCtx.Alipay == nil {
		l.Logger.Error("alipay not configured")
		return &pb.HandleNotifyResp{Ok: false, Message: "alipay not configured"}, nil
	}
	if len(in.Params) == 0 {
		l.Logger.Errorf("return error: %v", errors.Invalid)
		return nil, errors.Invalid
	}

	// 验签并解析通知。
	noti, err := l.svcCtx.Alipay.DecodeNotify(l.ctx, in.Params)
	if err != nil {
		l.Logger.Errorf("verify notify failed: %v", err)
		return &pb.HandleNotifyResp{Ok: false, Message: "verify failed"}, nil
	}

	// 先验证通知属于本应用和本收款方
	if noti.AppId != l.svcCtx.Config.Alipay.AppId {
		l.Logger.Errorf("notify app_id mismatch: out_trade_no=%s", noti.OutTradeNo)
		return &pb.HandleNotifyResp{Ok: false, Message: "invalid app_id"}, nil
	}
	if noti.SellerId != l.svcCtx.Config.Alipay.SellerId {
		l.Logger.Errorf("seller_id mismatch: out_trade_no=%s", noti.OutTradeNo)
		return &pb.HandleNotifyResp{Ok: false, Message: "invalid seller_id"}, nil
	}
	if noti.OutTradeNo == "" {
		l.Logger.Errorf("notify out_trade_no empty")
		return &pb.HandleNotifyResp{Ok: false, Message: "invalid order"}, nil
	}

	record, err := l.svcCtx.PaymentStore.FindByOutTradeNo(l.ctx, noti.OutTradeNo)
	if err != nil {
		l.Logger.Errorf("find payment failed: %v", err)
		return &pb.HandleNotifyResp{Ok: false, Message: "internal error"}, nil
	}
	if record == nil {
		l.Logger.Errorf("notify for unknown out_trade_no: %s", noti.OutTradeNo)
		return &pb.HandleNotifyResp{Ok: false, Message: "unknown order"}, nil
	}

	amountFen, err := infraalipay.YuanToFen(noti.TotalAmount)
	if err != nil || amountFen != record.Amount {
		l.Logger.Errorf(
			"notify amount mismatch: out_trade_no=%s, notify_amount=%s, expected_amount=%d",
			noti.OutTradeNo, noti.TotalAmount, record.Amount,
		)
		return &pb.HandleNotifyResp{Ok: false, Message: "amount mismatch"}, nil
	}

	// 仅在交易成功/结束时确认。其它状态（等待付款、关闭）仅记录。
	switch noti.TradeStatus {
	case alipay.TradeStatusSuccess, alipay.TradeStatusFinished:
		if noti.TradeNo == "" {
			l.Logger.Errorf("notify trade_no is empty: out_trade_no: %s", noti.OutTradeNo)
			return &pb.HandleNotifyResp{Ok: false, Message: "invalid trade_no"}, nil
		}

		if err := validateSuccessfulNotify(noti, record, l.svcCtx.Config.Alipay); err != nil {
			l.Logger.Errorf("reject successful notify: out_trade_no=%s error=%v", noti.OutTradeNo, err)
			return &pb.HandleNotifyResp{Ok: false, Message: "invalid notify"}, nil
		}

		raw, _ := json.Marshal(in.Params)
		if err := markPaid(l.ctx, l.svcCtx, record, noti.TradeNo, noti.BuyerId, string(raw)); err != nil {
			l.Logger.Errorf("mark paid failed: %v", err)
			return &pb.HandleNotifyResp{Ok: false, Message: "internal error"}, nil
		}
	case alipay.TradeStatusClosed:
		if record.Status == payments.StatusPending {
			record.Status = payments.StatusClosed
			if err := l.svcCtx.PaymentStore.Update(l.ctx, record); err != nil {
				l.Logger.Errorf("update closed payment failed: %v", err)
			}
		}
	default:
		l.Logger.Infof("notify trade_status=%s for %s, ignored", noti.TradeStatus, noti.OutTradeNo)
	}

	return &pb.HandleNotifyResp{Ok: true, Message: "success"}, nil
}
