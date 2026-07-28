package paymentservicelogic

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/url"
	"testing"

	infraali "budgetmatch-sim/infra/alipay"
	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/model/payments"
	"budgetmatch-sim/services/rpc/payment/pb"

	alipaysdk "github.com/smartwalle/alipay/v3"
	"github.com/smartwalle/nsign"
	"github.com/stretchr/testify/require"
)

type fakePaymentsModel struct {
	record      *payments.Payments
	findCalls   int
	updateCalls int
	updateErr   error
}

func (m *fakePaymentsModel) CreateTable() error {
	return nil
}

func (m *fakePaymentsModel) InsertOne(context.Context, *payments.Payments) error {
	return nil
}

func (m *fakePaymentsModel) FindOne(context.Context, string) (*payments.Payments, error) {
	return nil, nil
}

func (m *fakePaymentsModel) FindByOutTradeNo(_ context.Context, outTradeNo string) (*payments.Payments, error) {
	m.findCalls++
	if m.record == nil || m.record.OutTradeNo != outTradeNo {
		return nil, nil
	}
	return m.record, nil
}

func (m *fakePaymentsModel) FindByOrderID(context.Context, string) (*payments.Payments, error) {
	return nil, nil
}

func (m *fakePaymentsModel) Update(_ context.Context, record *payments.Payments) error {
	m.updateCalls++
	m.record = record
	return m.updateErr
}

func TestHandleNotify(t *testing.T) {
	aliClient, aliPrivateKey := newTestAlipayClient(t)

	t.Run("invalid signature does not update payment", func(t *testing.T) {
		store := &fakePaymentsModel{
			record: pendingPayment(),
		}
		logic := NewHandleNotifyLogic(context.Background(), &svc.ServiceContext{
			Alipay:       aliClient,
			PaymentStore: store,
		})
		params := notifyParams()
		params["sign"] = base64.StdEncoding.EncodeToString([]byte("invalid-signature"))

		resp, err := logic.HandleNotify(&pb.HandleNotifyReq{Params: params})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.Ok)
		require.Equal(t, "verify failed", resp.Message)
		require.Zero(t, store.findCalls, "验签失败后不应查询支付流水")
		require.Zero(t, store.updateCalls, "验签失败后不应更新支付流水")
		require.Equal(t, payments.StatusPending, store.record.Status)
	})

	t.Run("valid successful notification marks payment paid", func(t *testing.T) {
		store := &fakePaymentsModel{
			record: pendingPayment(),
		}
		logic := NewHandleNotifyLogic(context.Background(), &svc.ServiceContext{
			Alipay:       aliClient,
			PaymentStore: store,
		})
		params := signedNotifyParams(t, aliPrivateKey)

		resp, err := logic.HandleNotify(&pb.HandleNotifyReq{Params: params})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.True(t, resp.Ok)
		require.Equal(t, "success", resp.Message)
		require.Equal(t, 1, store.findCalls)
		require.Equal(t, 1, store.updateCalls)
		require.Equal(t, payments.StatusSuccess, store.record.Status)
		require.Equal(t, "alipay-trade-1", store.record.TradeNo)
		require.Equal(t, "buyer-1", store.record.BuyerId)
		require.NotNil(t, store.record.PaidAt)
		require.NotEmpty(t, store.record.NotifyRaw)
	})

	t.Run("duplicate notification only updates payment once", func(t *testing.T) {
		store := &fakePaymentsModel{
			record: pendingPayment(),
		}
		logic := NewHandleNotifyLogic(context.Background(), &svc.ServiceContext{
			Alipay:       aliClient,
			PaymentStore: store,
		})
		req := &pb.HandleNotifyReq{Params: signedNotifyParams(t, aliPrivateKey)}

		firstResp, firstErr := logic.HandleNotify(req)
		secondResp, secondErr := logic.HandleNotify(req)

		require.NoError(t, firstErr)
		require.NoError(t, secondErr)
		require.True(t, firstResp.Ok)
		require.True(t, secondResp.Ok)
		require.Equal(t, 2, store.findCalls)
		require.Equal(t, 1, store.updateCalls, "重复通知不应重复更新支付流水")
		require.Equal(t, payments.StatusSuccess, store.record.Status)
	})
}

func newTestAlipayClient(t *testing.T) (*infraali.Client, *rsa.PrivateKey) {
	t.Helper()

	appPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	aliPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	aliPublicKey, err := x509.MarshalPKIXPublicKey(&aliPrivateKey.PublicKey)
	require.NoError(t, err)

	client, err := infraali.NewClient(infraali.Config{
		AppID:           "test-app",
		PrivateKey:      base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(appPrivateKey)),
		AlipayPublicKey: base64.StdEncoding.EncodeToString(aliPublicKey),
		IsProduction:    false,
	})
	require.NoError(t, err)
	require.NotNil(t, client)
	return client, aliPrivateKey
}

func signedNotifyParams(t *testing.T, aliPrivateKey *rsa.PrivateKey) map[string]string {
	t.Helper()

	values := url.Values{}
	for key, value := range notifyParams() {
		values.Set(key, value)
	}
	signer := nsign.New(
		nsign.WithMethod(nsign.NewRSAMethod(crypto.SHA256, aliPrivateKey, nil)),
		nsign.WithEncoder(alipaysdk.Encoder{}),
	)
	signature, err := signer.SignValues(
		values,
		nsign.WithIgnore("sign", "sign_type", "alipay_cert_sn"),
	)
	require.NoError(t, err)
	values.Set("sign", base64.StdEncoding.EncodeToString(signature))

	params := make(map[string]string, len(values))
	for key := range values {
		params[key] = values.Get(key)
	}
	return params
}

func notifyParams() map[string]string {
	return map[string]string{
		"app_id":         "test-app",
		"charset":        "utf-8",
		"notify_id":      "notify-1",
		"notify_time":    "2026-07-26 20:00:00",
		"notify_type":    "trade_status_sync",
		"out_trade_no":   "payment-1",
		"trade_no":       "alipay-trade-1",
		"trade_status":   string(alipaysdk.TradeStatusSuccess),
		"buyer_id":       "buyer-1",
		"total_amount":   "10.00",
		"receipt_amount": "10.00",
		"sign_type":      "RSA2",
	}
}

func pendingPayment() *payments.Payments {
	return &payments.Payments{
		Id:         "pay-1",
		OutTradeNo: "payment-1",
		OrderId:    "order-1",
		UserId:     "user-1",
		Amount:     1000,
		Channel:    "alipay",
		Status:     payments.StatusPending,
	}
}
