package paymentservicelogic

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"
)

type CreatePaymentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreatePaymentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatePaymentLogic {
	return &CreatePaymentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreatePayment 发起支付，返回支付宝当面付二维码。
func (l *CreatePaymentLogic) CreatePayment(in *pb.CreatePaymentReq) (*pb.CreatePaymentResp, error) {
	if in.OrderId == "" || in.UserId == "" || in.Amount <= 0 {
		l.Logger.Errorf("return error: %v", errors.Invalid)
		return nil, errors.Invalid
	}
	if l.svcCtx.Alipay == nil {
		l.Logger.Error("alipay not configured")
		return nil, errors.Internal
	}

	// 幂等：同一订单已有流水时复用。
	existing, err := l.svcCtx.PaymentStore.FindByOrderID(l.ctx, in.OrderId)
	if err != nil {
		l.Logger.Errorf("find payment by order failed: %v", err)
		return nil, errors.Database
	}
	if existing != nil && existing.Status == payments.StatusSuccess {
		// 已支付，直接返回成功态（无需再出二维码）。
		return &pb.CreatePaymentResp{
			OutTradeNo: existing.OutTradeNo,
			Status:     int32(existing.Status),
		}, nil
	}

	// 复用待支付流水的商户订单号，避免重复建单；否则新建一笔。
	var record *payments.Payments
	if existing != nil && existing.Status == payments.StatusPending {
		record = existing
	} else {
		record = &payments.Payments{
			Id:         payments.NewPaymentId(),
			OutTradeNo: genOutTradeNo(),
			OrderId:    in.OrderId,
			UserId:     in.UserId,
			Amount:     in.Amount,
			Channel:    "alipay",
			Status:     payments.StatusPending,
			NotifyRaw:  "{}",
		}
		if err := l.svcCtx.PaymentStore.InsertOne(l.ctx, record); err != nil {
			l.Logger.Errorf("insert payment failed: %v", err)
			return nil, errors.Database
		}
	}

	subject := in.Subject
	if subject == "" {
		subject = "订单 " + in.OrderId
	}

	// 调支付宝当面付预下单，拿到二维码码串。
	res, err := l.svcCtx.Alipay.PreCreate(l.ctx, record.OutTradeNo, subject, record.Amount)
	if err != nil {
		l.Logger.Errorf("alipay precreate failed: %v", err)
		return nil, errors.Internal
	}

	record.QrCode = res.QRCode
	if err := l.svcCtx.PaymentStore.Update(l.ctx, record); err != nil {
		l.Logger.Errorf(
			"save QRCode failed, returning pre-created payment anyway: order=%s outTradeNo=%s error=%v",
			record.OrderId,
			record.OutTradeNo,
			err,
		)
	}
	return &pb.CreatePaymentResp{
		OutTradeNo: record.OutTradeNo,
		QrCode:     res.QRCode,
		Status:     int32(payments.StatusPending),
	}, nil
}

// genOutTradeNo 生成商户订单号（去横线 uuid，32 位，满足支付宝 ≤64 位要求）。
func genOutTradeNo() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}
