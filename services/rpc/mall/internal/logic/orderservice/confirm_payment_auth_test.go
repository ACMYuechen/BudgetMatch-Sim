package orderservicelogic

import (
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// validConfigPaymentReq 返回通过基础参数校验的请求
func validConfigPaymentReq() *pb.ConfirmPaymentReq {
	return &pb.ConfirmPaymentReq{
		OrderId:    "order-1",
		UserId:     "user-1",
		Amount:     100,
		OutTradeNo: "out-trade-1",
		TradeNo:    "trade-1",
	}
}

// TestConfirmPaymentRejectsMissingServiceIdentity 验证无服务身份时立即拒绝
func TestConfirmPaymentRejectsMissingServiceIdentity(t *testing.T) {
	logic := NewConfirmPaymentLogic(context.Background(), &svc.ServiceContext{})
	resp, err := logic.ConfirmPayment(validConfigPaymentReq())

	assert.ErrorIs(t, err, apperrors.Unauthorized)
	assert.Nil(t, resp)
}

// TestConfirmPaymentRejectsWrongServiceIdentity 验证其他服务不能确认支付
func TestConfirmPaymentRejectsWrongServiceIdentity(t *testing.T) {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyServiceName, "agent-rpc")
	logic := NewConfirmPaymentLogic(ctx, &svc.ServiceContext{})
	resp, err := logic.ConfirmPayment(validConfigPaymentReq())

	assert.ErrorIs(t, err, apperrors.Unauthorized)
	assert.Nil(t, resp)
}

// TestConfirmPaymentAcceptsPaymentIdentity 验证 payment-rpc 身份能够通过身份检查
func TestConfirmPaymentAcceptsPaymentIdentity(t *testing.T) {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyServiceName, serviceauth.ServicePayment)
	logic := NewConfirmPaymentLogic(ctx, &svc.ServiceContext{})

	// nil请求应该进入参数校验并返回 Invalid，证明通过了前面的服务身份检查
	resp, err := logic.ConfirmPayment(nil)

	assert.ErrorIs(t, err, apperrors.Invalid)
	assert.Nil(t, resp)
}
