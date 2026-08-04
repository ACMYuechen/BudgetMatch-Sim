package mall

import (
	"context"
	"testing"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/client/orderservice"
	mallpb "budgetmatch-sim/services/rpc/mall/pb"
	"budgetmatch-sim/services/rpc/payment/client/paymentservice"
	paymentpb "budgetmatch-sim/services/rpc/payment/pb"

	"google.golang.org/grpc"
)

type fakeOrderClient struct {
	orderservice.OrderService
	getOrder func(context.Context, *mallpb.GetOrderReq) (*mallpb.GetOrderResp, error)
}

func (f *fakeOrderClient) GetOrder(ctx context.Context, req *mallpb.GetOrderReq, _ ...grpc.CallOption) (*mallpb.GetOrderResp, error) {
	return f.getOrder(ctx, req)
}

type fakePaymentClient struct {
	paymentservice.PaymentService
	createPayment func(context.Context, *paymentpb.CreatePaymentReq) (*paymentpb.CreatePaymentResp, error)
	queryPayment  func(context.Context, *paymentpb.QueryPaymentReq) (*paymentpb.QueryPaymentResp, error)
}

func (f *fakePaymentClient) CreatePayment(ctx context.Context, req *paymentpb.CreatePaymentReq, _ ...grpc.CallOption) (*paymentpb.CreatePaymentResp, error) {
	return f.createPayment(ctx, req)
}

func (f *fakePaymentClient) QueryPayment(ctx context.Context, req *paymentpb.QueryPaymentReq, _ ...grpc.CallOption) (*paymentpb.QueryPaymentResp, error) {
	return f.queryPayment(ctx, req)
}

func validPaymentOrder() *mallpb.Order {
	return &mallpb.Order{
		Id:             "order-1",
		UserId:         "user-1",
		OriginalAmount: 1200,
		DiscountAmount: 200,
		PayAmount:      1000,
		Status:         MallOrderStatusPending,
	}
}

func paymentContext() context.Context {
	return context.WithValue(context.Background(), "user_id", "user-1")
}

func TestCreatePaymentSuccess(t *testing.T) {
	order := validPaymentOrder()
	orderClient := &fakeOrderClient{
		getOrder: func(_ context.Context, req *mallpb.GetOrderReq) (*mallpb.GetOrderResp, error) {
			if req.OrderId != order.Id || req.UserId != order.UserId {
				t.Fatalf("unexpected GetOrder request: %+v", req)
			}
			return &mallpb.GetOrderResp{Order: order}, nil
		},
	}
	paymentClient := &fakePaymentClient{
		createPayment: func(_ context.Context, req *paymentpb.CreatePaymentReq) (*paymentpb.CreatePaymentResp, error) {
			if req.OrderId != order.Id || req.UserId != order.UserId || req.Amount != order.PayAmount {
				t.Fatalf("unexpected CreatePayment request: %+v", req)
			}
			return &paymentpb.CreatePaymentResp{
				OutTradeNo: "trade-1",
				QrCode:     "https://qr.example/trade-1",
				Status:     0,
			}, nil
		},
	}
	logic := NewCreatePaymentLogic(paymentContext(), &svc.ServiceContext{
		MallOrderClient: orderClient,
		PaymentClient:   paymentClient,
	})

	resp, err := logic.CreatePayment(&types.MallCreatePaymentReq{Id: order.Id})
	if err != nil {
		t.Fatalf("CreatePayment returned error: %v", err)
	}
	if resp.OutTradeNo != "trade-1" || resp.QrCode == "" || resp.Status != 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreatePaymentRejectsAnotherUsersOrder(t *testing.T) {
	order := validPaymentOrder()
	order.UserId = "user-2"
	logic := NewCreatePaymentLogic(paymentContext(), &svc.ServiceContext{
		MallOrderClient: &fakeOrderClient{
			getOrder: func(context.Context, *mallpb.GetOrderReq) (*mallpb.GetOrderResp, error) {
				return &mallpb.GetOrderResp{Order: order}, nil
			},
		},
	})

	_, err := logic.CreatePayment(&types.MallCreatePaymentReq{Id: order.Id})
	if err != errors.MallOrderNotFound {
		t.Fatalf("got error %v, want %v", err, errors.MallOrderNotFound)
	}
}

func TestCreatePaymentRejectsNonPendingOrder(t *testing.T) {
	order := validPaymentOrder()
	order.Status = 2
	logic := NewCreatePaymentLogic(paymentContext(), &svc.ServiceContext{
		MallOrderClient: &fakeOrderClient{
			getOrder: func(context.Context, *mallpb.GetOrderReq) (*mallpb.GetOrderResp, error) {
				return &mallpb.GetOrderResp{Order: order}, nil
			},
		},
	})

	_, err := logic.CreatePayment(&types.MallCreatePaymentReq{Id: order.Id})
	if err != errors.MallInvalidOrderTransition {
		t.Fatalf("got error %v, want %v", err, errors.MallInvalidOrderTransition)
	}
}

func TestCreatePaymentRejectsInvalidOrderAmount(t *testing.T) {
	order := validPaymentOrder()
	order.PayAmount = 999
	logic := NewCreatePaymentLogic(paymentContext(), &svc.ServiceContext{
		MallOrderClient: &fakeOrderClient{
			getOrder: func(context.Context, *mallpb.GetOrderReq) (*mallpb.GetOrderResp, error) {
				return &mallpb.GetOrderResp{Order: order}, nil
			},
		},
	})

	_, err := logic.CreatePayment(&types.MallCreatePaymentReq{Id: order.Id})
	if err != errors.Invalid {
		t.Fatalf("got error %v, want %v", err, errors.Invalid)
	}
}

func TestQueryPaymentSuccess(t *testing.T) {
	order := validPaymentOrder()
	payment := &paymentpb.Payment{
		Id:         "payment-1",
		OutTradeNo: "trade-1",
		OrderId:    order.Id,
		UserId:     order.UserId,
		Amount:     order.PayAmount,
		Channel:    "alipay",
		Status:     1,
		TradeNo:    "alipay-trade-1",
		PaidAt:     "2026-07-23T12:00:00+08:00",
		QrCode:     "https://qr.example/trade-1",
	}
	logic := NewQueryPaymentLogic(paymentContext(), &svc.ServiceContext{
		MallOrderClient: &fakeOrderClient{
			getOrder: func(context.Context, *mallpb.GetOrderReq) (*mallpb.GetOrderResp, error) {
				return &mallpb.GetOrderResp{Order: order}, nil
			},
		},
		PaymentClient: &fakePaymentClient{
			queryPayment: func(_ context.Context, req *paymentpb.QueryPaymentReq) (*paymentpb.QueryPaymentResp, error) {
				if req.OrderId != order.Id {
					t.Fatalf("unexpected QueryPayment request: %+v", req)
				}
				return &paymentpb.QueryPaymentResp{
					Status:  payment.Status,
					TradeNo: payment.TradeNo,
					Payment: payment,
				}, nil
			},
		},
	})

	resp, err := logic.QueryPayment(&types.MallQueryPaymentReq{Id: order.Id})
	if err != nil {
		t.Fatalf("QueryPayment returned error: %v", err)
	}
	if resp.Status != payment.Status ||
		resp.TradeNo != payment.TradeNo ||
		resp.Payment.OutTradeNo != payment.OutTradeNo ||
		resp.Payment.Amount != order.PayAmount ||
		resp.Payment.QrCode != "https://qr.example/trade-1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestQueryPaymentRejectsMismatchedPayment(t *testing.T) {
	order := validPaymentOrder()
	logic := NewQueryPaymentLogic(paymentContext(), &svc.ServiceContext{
		MallOrderClient: &fakeOrderClient{
			getOrder: func(context.Context, *mallpb.GetOrderReq) (*mallpb.GetOrderResp, error) {
				return &mallpb.GetOrderResp{Order: order}, nil
			},
		},
		PaymentClient: &fakePaymentClient{
			queryPayment: func(context.Context, *paymentpb.QueryPaymentReq) (*paymentpb.QueryPaymentResp, error) {
				return &paymentpb.QueryPaymentResp{
					Status: 0,
					Payment: &paymentpb.Payment{
						OrderId: order.Id,
						UserId:  order.UserId,
						Amount:  order.PayAmount + 1,
						Status:  0,
					},
				}, nil
			},
		},
	})

	_, err := logic.QueryPayment(&types.MallQueryPaymentReq{Id: order.Id})
	if err != errors.Internal {
		t.Fatalf("got error %v, want %v", err, errors.Internal)
	}
}
