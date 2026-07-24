package paymentservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// markPaid 幂等地把一笔流水标记为支付成功，并落库支付宝交易号等信息。
// 若流水已是成功态则直接返回（幂等），不重复触发后续动作。
//
// TODO(接订单): 支付成功后需通知 mall-rpc 把对应订单状态置为「已支付」。
// 待后续给 mall 增加 ConfirmPayment RPC 后，在此处调用（建议放在事务/重试语义下，
// 并保证 notify 与 query 两条确认路径都经过这里，避免重复确认）。
func markPaid(ctx context.Context, svcCtx *svc.ServiceContext, record *payments.Payments, tradeNo, buyerID, rawNotify string) error {
	if record.Status == payments.StatusSuccess {
		return nil
	}
	now := time.Now()
	record.Status = payments.StatusSuccess
	record.TradeNo = tradeNo
	record.BuyerId = buyerID
	record.PaidAt = &now
	if rawNotify != "" {
		record.NotifyRaw = rawNotify
	}
	if err := svcCtx.PaymentStore.Update(ctx, record); err != nil {
		logx.WithContext(ctx).Errorf("update payment failed: %v", err)
		return err
	}
	logx.WithContext(ctx).Infof("payment %s (order %s) marked paid, tradeNo=%s", record.OutTradeNo, record.OrderId, tradeNo)
	return nil
}

func paymentToPb(p *payments.Payments) *pb.Payment {
	if p == nil {
		return nil
	}
	resp := &pb.Payment{
		Id:         p.Id,
		OutTradeNo: p.OutTradeNo,
		OrderId:    p.OrderId,
		UserId:     p.UserId,
		Amount:     p.Amount,
		Channel:    p.Channel,
		Status:     int32(p.Status),
		TradeNo:    p.TradeNo,
		BuyerId:    p.BuyerId,
		CreatedAt:  p.CreatedAt.Format(timeLayout),
		UpdatedAt:  p.UpdatedAt.Format(timeLayout),
	}
	if p.PaidAt != nil {
		resp.PaidAt = p.PaidAt.Format(timeLayout)
	}
	return resp
}
