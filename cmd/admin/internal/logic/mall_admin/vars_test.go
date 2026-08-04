package mall_admin

import (
	"budgetmatch-sim/services/rpc/mall/pb"
	"testing"
)

func TestOrderToTypeCopiesPaymentFields(t *testing.T) {
	order := &pb.Order{
		Id:            "mord-test",
		PaymentStatus: pb.PaymentStatus_PAYMENT_STATUS_PAID,
		OutTradeNo:    "out-test-001",
		TradeNo:       "trade-test-001",
		PayTime:       "2026-08-04T12:00:00+08:00",
	}
	got := orderToType(order)

	if got.PaymentStatus != 2 {
		t.Fatalf("PaymentStatus = %v, want 2", got.PaymentStatus)
	}
	if got.OutTradeNo != order.OutTradeNo {
		t.Fatalf("OutTradeNo = %q, want %q", got.OutTradeNo, order.OutTradeNo)
	}
	if got.TradeNo != order.TradeNo {
		t.Fatalf("TradeNo = %q, want %q", got.TradeNo, order.TradeNo)
	}
	if got.PayTime != order.PayTime {
		t.Fatalf("PayTime = %v, want %v", got.PayTime, order.PayTime)
	}
}
