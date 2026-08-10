package paymentservicelogic

import (
	mallpb "budgetmatch-sim/services/rpc/mall/pb"
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

	userId, err := authenticatedUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("get authenticated user failed: %v", errors.Invalid)
		return nil, err
	}

	if in.UserId != userId {
		l.Logger.Errorf("create payment user mismatch: authenticated_user_id=%s request_user_id=%s order_id=%s",
			userId, in.UserId, in.OrderId)
		// 不暴露目标订单是否存在
		return nil, errors.MallOrderNotFound
	}
	if l.svcCtx.Alipay == nil {
		l.Logger.Error("alipay not configured")
		return nil, errors.Internal
	}

	mallCtx, err := newMallUserContext(l.ctx)
	if err != nil {
		l.Logger.Errorf("create mall user context failed: %v", err)
		return nil, err
	}

	orderResp, err := l.svcCtx.OrderRpc.GetOrder(mallCtx, &mallpb.GetOrderReq{
		OrderId: in.OrderId,
		UserId:  userId,
	},
	)
	if err != nil {
		l.Logger.Errorf("get payment order failed: order_id=%d %v",
			in.OrderId, userId, err)
		return nil, err
	}
	if orderResp == nil || orderResp.Order == nil {
		l.Logger.Errorf("mall returned empty order: order_id=%d %s", in.OrderId)
		return nil, errors.MallOrderNotFound
	}

	order := orderResp.Order
	if order.Id != in.OrderId || order.UserId != userId {
		l.Logger.Errorf("payment order ownership mismatch: order_id=%s authenticated_user_id=%s order_user_id=%s",
			in.OrderId, userId, order.UserId)
		return nil, errors.MallOrderNotFound
	}

	if order.PayAmount <= 0 || order.OriginalAmount < 0 ||
		order.DiscountAmount < 0 || order.PayAmount != order.OriginalAmount-order.DiscountAmount {
		l.Logger.Errorf("invalid mall order amount: order_id=%s", order.Id)
		return nil, errors.Invalid
	}
	if in.Amount != order.PayAmount {
		l.Logger.Errorf("create payment amount mismatch: order_id=%s request_amount=%d order_amount=%d",
			order.Id, in.Amount, order.PayAmount)
		return nil, errors.MallPaymentAmountMismatch
	}

	// 幂等：同一订单已有流水时复用。
	existing, err := l.svcCtx.PaymentStore.FindByOrderId(l.ctx, in.OrderId)
	if err != nil {
		l.Logger.Errorf("find payment by order failed: %v", err)
		return nil, errors.Database
	}

	// 检查旧流水归属
	if existing != nil && (existing.UserId != userId || existing.Amount != order.PayAmount) {
		l.Logger.Errorf("existing payment does not match order: order_id=%s payment_user_id=%s payment_amount=%d",
			order.Id, existing.UserId, existing.Amount)
		return nil, errors.Conflict
	}
	if existing != nil && existing.Status == payments.StatusSuccess {
		// 已支付，直接返回成功态（无需再出二维码）。
		return &pb.CreatePaymentResp{
			OutTradeNo: existing.OutTradeNo,
			Status:     int32(existing.Status),
		}, nil
	}

	// 复用或创建待支付流水之前检查订单状态
	if order.Status != mallpb.OrderStatus_ORDER_STATUS_PENDING {
		l.Logger.Errorf("order is not pending: order_id=%s status=%d",
			order.Id, order.Status)
		return nil, errors.MallInvalidOrderTransition
	}

	// 复用待支付流水的商户订单号，避免重复建单；否则新建一笔。
	var record *payments.Payments
	if existing != nil && existing.Status == payments.StatusPending {
		record = existing
	} else {
		record = &payments.Payments{
			Id:         payments.NewPaymentId(),
			OutTradeNo: genOutTradeNo(),
			OrderId:    order.Id,
			UserId:     userId,
			Amount:     order.PayAmount,
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

	// 支付宝预下单已经成功，二维码持久化仅用于后续查询。即使数据库更新失败，
	// 也应优先将本次生成的二维码返回给调用方，避免调用方因重试而重复发起预下单。
	record.QrCode = res.QrCode
	if err := l.svcCtx.PaymentStore.Update(l.ctx, record); err != nil {
		l.Logger.Errorf(
			"save QrCode failed, returning pre-created payment anyway: order=%s outTradeNo=%s error=%v",
			record.OrderId,
			record.OutTradeNo,
			err,
		)
	}

	return &pb.CreatePaymentResp{
		OutTradeNo: record.OutTradeNo,
		QrCode:     res.QrCode,
		Status:     int32(payments.StatusPending),
	}, nil
}

// genOutTradeNo 生成商户订单号（去横线 uuid，32 位，满足支付宝 ≤64 位要求）。
func genOutTradeNo() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}
