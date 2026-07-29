package paymentservicelogic

import (
	mallpb "budgetmatch-sim/services/rpc/mall/pb"
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/metadata"

	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// markPaid 幂等地把一笔流水标记为支付成功，并落库支付宝交易号等信息。
// 若流水已是成功态则直接返回（幂等），不重复触发后续动作。
func markPaid(ctx context.Context, svcCtx *svc.ServiceContext, record *payments.Payments, tradeNo, buyerID, rawNotify string) error {
	now := time.Now()
	candidate := *record
	candidate.Status = payments.StatusSuccess
	candidate.TradeNo = tradeNo
	candidate.BuyerId = buyerID
	candidate.PaidAt = &now
	update, err := svcCtx.PaymentStore.MarkPaidIfPending(ctx, record)
	if err != nil {
		logx.WithContext(ctx).Errorf("conditional update payment %s (order %s) failed: %v", record.OutTradeNo, record.OrderId, err)
		return err
	}

	if !update {
		// 可能是流水关闭等不可支付状态，也可能是另一个并发请求已经更新为成功
		latest, err := svcCtx.PaymentStore.FindOne(ctx, record.Id)
		if err != nil {
			logx.WithContext(ctx).Errorf("failed to query payment %s after conditional update: err=%v", record.Id, err)
			return err
		}
		if latest.Status != payments.StatusSuccess {
			err := fmt.Errorf("payment %s is not payable: status=%d", latest.OutTradeNo, latest.Status)
			logx.WithContext(ctx).Error(err)
			return err
		}
		*record = *latest
		logx.WithContext(ctx).Infof("payment %s (order %s) already marked as paid by other request", record.OutTradeNo, record.OrderId)
	} else {
		*record = candidate
		if rawNotify != "" {
			record.NotifyRaw = rawNotify
		}
		logx.WithContext(ctx).Infof("payment %s (order %s) marked paid, tradeNo=%s", record.OutTradeNo, record.OrderId, tradeNo)
	}

	mallCtx := forwardAuthorization(ctx)

	confirmResp, err := svcCtx.OrderRpc.ConfirmPayment(mallCtx, &mallpb.ConfirmPaymentReq{
		OrderId:    record.OrderId,
		UserId:     record.UserId,
		Amount:     record.Amount,
		OutTradeNo: record.OutTradeNo,
		TradeNo:    record.TradeNo,
	})

	if err != nil {
		logx.WithContext(ctx).Errorf("confirm payment %s (order %s) failed: %v", record.OutTradeNo, record.OrderId, err)
		return err
	}
	logx.WithContext(ctx).Infof("order payment %s (order %s) confirmed", record.OutTradeNo, record.OrderId)

	if confirmResp == nil || !confirmResp.Success {
		err := fmt.Errorf("mall confirm payment %s (order %s) failed", record.OutTradeNo, record.OrderId)
		logx.WithContext(ctx).Error(err)
		return err
	}
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
		QrCode:     p.QrCode,
	}
	if p.PaidAt != nil {
		resp.PaidAt = p.PaidAt.Format(timeLayout)
	}
	return resp
}

// forwardAuthorization 将当前请求的用户认证信息转发给下游 RPC。
func forwardAuthorization(ctx context.Context) context.Context {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	authorization := incoming.Get("authorization")
	if len(authorization) == 0 {
		return ctx
	}

	outgoing := metadata.MD{}
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		outgoing = md.Copy()
	}
	outgoing.Set("authorization", authorization...)

	return metadata.NewOutgoingContext(ctx, outgoing)
}
