package paymentservicelogic

import (
	infraalipay "budgetmatch-sim/infra/alipay"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
	paymentconfig "budgetmatch-sim/services/rpc/payment/internal/config"
	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"
	"context"
	"strings"
	"testing"

	sdkalipay "github.com/smartwalle/alipay/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

const callbackServiceSecret = "callback-service-secret-for-unit-testing"

// TestNewMallCallbackContext 验证payment-rpc 生成正确的 mall 服务 Token
func TestNewMallCallbackContext(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		Config: paymentconfig.Config{
			ServiceAuth: serviceauth.Config{
				Secret: callbackServiceSecret,
			},
		},
	}

	ctx, err := newMallCallbackContext(context.Background(), svcCtx)
	require.NoError(t, err)

	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)

	values := md.Get("authorization")
	require.Len(t, values, 1)
	require.True(t, strings.HasPrefix(values[0], "Bearer "))

	token := strings.TrimPrefix(values[0], "Bearer ")
	claims, err := serviceauth.ValidateToken(
		token, callbackServiceSecret, serviceauth.ServicePayment, serviceauth.ServiceMall,
	)

	require.NoError(t, err)
	assert.Equal(t, serviceauth.ServicePayment, claims.Service)
	assert.True(t, claims.VerifyAudience(serviceauth.ServiceMall, true))
}

// TestNewMallCallbackContextRejectsEmptySecret 验证空密钥不能生成服务 Token
func TestNewMallCallbackContextRejectsEmptySecret(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		Config: paymentconfig.Config{
			ServiceAuth: serviceauth.Config{},
		},
	}

	ctx, err := newMallCallbackContext(context.Background(), svcCtx)

	assert.Error(t, err)
	assert.Nil(t, ctx)
}

// TestValidateSuccessfulNotify 验证验签成功后的关键业务字段必须全部匹配。
func TestValidateSuccessfulNotify(t *testing.T) {
	cfg := infraalipay.Config{AppId: "app-1", SellerId: "seller-1"}
	baseRecord := payments.Payments{
		OutTradeNo: "out-trade-1",
		Amount:     996,
		Status:     payments.StatusPending,
	}
	baseNotify := sdkalipay.Notification{
		AppId:       "app-1",
		SellerId:    "seller-1",
		OutTradeNo:  "out-trade-1",
		TradeNo:     "trade-1",
		TradeStatus: sdkalipay.TradeStatusSuccess,
		TotalAmount: "9.96",
	}

	tests := []struct {
		name         string
		mutateNotify func(*sdkalipay.Notification)
		mutateRecord func(*payments.Payments)
		wantErr      bool
	}{
		{name: "valid successful notify", wantErr: false},
		{name: "app id mismatch", mutateNotify: func(n *sdkalipay.Notification) { n.AppId = "other-app" }, wantErr: true},
		{name: "seller id mismatch", mutateNotify: func(n *sdkalipay.Notification) { n.SellerId = "other-seller" }, wantErr: true},
		{name: "amount mismatch", mutateNotify: func(n *sdkalipay.Notification) { n.TotalAmount = "9.95" }, wantErr: true},
		{name: "invalid amount precision", mutateNotify: func(n *sdkalipay.Notification) { n.TotalAmount = "9.960" }, wantErr: true},
		{name: "out trade number mismatch", mutateNotify: func(n *sdkalipay.Notification) { n.OutTradeNo = "other-out-trade" }, wantErr: true},
		{name: "empty trade number", mutateNotify: func(n *sdkalipay.Notification) { n.TradeNo = "" }, wantErr: true},
		{name: "waiting trade status", mutateNotify: func(n *sdkalipay.Notification) { n.TradeStatus = sdkalipay.TradeStatusWaitBuyerPay }, wantErr: true},
		{
			name: "confirmed trade number conflict",
			mutateRecord: func(record *payments.Payments) {
				record.Status = payments.StatusSuccess
				record.TradeNo = "other-trade"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := baseRecord
			notify := baseNotify
			if tt.mutateNotify != nil {
				tt.mutateNotify(&notify)
			}
			if tt.mutateRecord != nil {
				tt.mutateRecord(&record)
			}

			err := validateSuccessfulNotify(&notify, &record, cfg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

type paymentStoreStub struct {
	payments.PaymentsModel
	record *payments.Payments
}

func (s *paymentStoreStub) FindByOrderId(context.Context, string) (*payments.Payments, error) {
	return s.record, nil
}

// TestCreatePaymentRejectsCrossUserRequest 验证请求字段不能覆盖认证上下文中的用户身份。
func TestCreatePaymentRejectsCrossUserRequest(t *testing.T) {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user-b")
	logic := NewCreatePaymentLogic(ctx, &svc.ServiceContext{})

	resp, err := logic.CreatePayment(&pb.CreatePaymentReq{
		OrderId: "order-a",
		UserId:  "user-a",
		Amount:  996,
	})

	assert.ErrorIs(t, err, apperrors.MallOrderNotFound)
	assert.Nil(t, resp)
}

// TestQueryPaymentRejectsCrossUserRecord 验证用户不能查询其他用户的支付流水。
func TestQueryPaymentRejectsCrossUserRecord(t *testing.T) {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user-b")
	logic := NewQueryPaymentLogic(ctx, &svc.ServiceContext{
		PaymentStore: &paymentStoreStub{record: &payments.Payments{
			OrderId:    "order-a",
			UserId:     "user-a",
			OutTradeNo: "out-trade-a",
			Amount:     996,
		}},
	})

	resp, err := logic.QueryPayment(&pb.QueryPaymentReq{OrderId: "order-a"})

	assert.ErrorIs(t, err, apperrors.NotFound)
	assert.Nil(t, resp)
}
