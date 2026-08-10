package paymentservicelogic

import (
	"budgetmatch-sim/infra/errors"
	"context"
	"fmt"
	"time"

	infraalipay "budgetmatch-sim/infra/alipay"
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/role"
	mallpb "budgetmatch-sim/services/rpc/mall/pb"
	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"

	sdkalipay "github.com/smartwalle/alipay/v3"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/metadata"
)

const (
	timeLayout              = "2006-01-02T15:04:05Z07:00"
	mallCallbackTokenExpire = int64(60)
)

// markPaid 幂等地把一笔流水标记为支付成功，并落库支付宝交易号等信息。
// 流水已成功时仍会幂等回写 mall，支持上一次订单回写失败后的重试。
func markPaid(ctx context.Context, svcCtx *svc.ServiceContext, record *payments.Payments, tradeNo, buyerId, rawNotify string) error {
	now := time.Now()
	candidate := *record
	candidate.Status = payments.StatusSuccess
	candidate.TradeNo = tradeNo
	candidate.BuyerId = buyerId
	candidate.PaidAt = &now
	if rawNotify != "" {
		candidate.NotifyRaw = rawNotify
	}
	update, err := svcCtx.PaymentStore.MarkPaidIfPending(ctx, &candidate)
	if err != nil {
		logx.WithContext(ctx).Errorf("conditional update payment %s (order %s) failed: %v", record.OutTradeNo, record.OrderId, err)
		return errors.Database
	}

	if !update {
		// 可能是流水关闭等不可支付状态，也可能是另一个并发请求已经更新为成功
		latest, err := svcCtx.PaymentStore.FindOne(ctx, record.Id)
		if err != nil {
			logx.WithContext(ctx).Errorf("failed to query payment %s after conditional update: err=%v", record.Id, err)
			return errors.Database
		}
		if latest.Status != payments.StatusSuccess {
			err := fmt.Errorf("payment %s is not payable: status=%d", latest.OutTradeNo, latest.Status)
			logx.WithContext(ctx).Error(err)
			return errors.Conflict
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

	mallCtx, err := newMallCallbackContext(ctx, svcCtx, record.UserId)
	if err != nil {
		logx.WithContext(ctx).Errorf("create mall callback context failed: payment=%s (order %s) error=%v", record.OutTradeNo, record.OrderId, err)
		return errors.TokenGeneration
	}

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
		return errors.Internal
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

// newMallCallbackContext 为 payment-rpc 回写订单生成短期调用凭据。
func newMallCallbackContext(ctx context.Context, svcCtx *svc.ServiceContext, userID string) (context.Context, error) {
	token, err := auth.GenerateToken(
		userID,
		svcCtx.Config.JwtAuth.Secret,
		mallCallbackTokenExpire,
		role.RoleUser,
	)
	if err != nil {
		return nil, fmt.Errorf("generate mall callback token failed: %w", err)
	}

	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token), nil
}

// validateSuccessfulNotify 校验已经通过签名验证的支付成功通知。
func validateSuccessfulNotify(noti *sdkalipay.Notification, record *payments.Payments, cfg infraalipay.Config) error {
	if noti.AppId == "" || noti.AppId != cfg.AppId {
		return fmt.Errorf("app_id mismatch")
	}
	if noti.SellerId == "" || noti.SellerId != cfg.SellerId {
		return fmt.Errorf("seller_id mismatch")
	}
	if noti.OutTradeNo == "" || noti.OutTradeNo != record.OutTradeNo {
		return fmt.Errorf("out_trade_no mismatch")
	}
	if noti.TradeNo == "" {
		return fmt.Errorf("trade_no is empty")
	}

	switch noti.TradeStatus {
	case sdkalipay.TradeStatusSuccess, sdkalipay.TradeStatusFinished:
	default:
		return fmt.Errorf("invalid success trade status: %s", noti.TradeStatus)
	}

	amountFen, err := infraalipay.YuanToFen(noti.TotalAmount)
	if err != nil {
		return fmt.Errorf("invalid total amount: %w", err)
	}
	if amountFen != record.Amount {
		return fmt.Errorf("total_amount mismatch: notify=%d, record=%d", amountFen, record.Amount)
	}

	if record.Status == payments.StatusSuccess &&
		record.TradeNo != "" &&
		record.TradeNo != noti.TradeNo {
		return fmt.Errorf("trade_no conflicts with confirmed payment")
	}
	return nil
}
