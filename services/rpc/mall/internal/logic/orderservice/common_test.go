package orderservicelogic

import (
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/pb"
	"testing"
	"time"
)

func TestPaymentStatusOf(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		order *mall_orders.MallOrders
		want  pb.PaymentStatus
	}{
		{
			name:  "支付信息全为空时未支付",
			order: &mall_orders.MallOrders{},
			want:  pb.PaymentStatus_PAYMENT_STATUS_UNPAID,
		},
		{
			name: "支付信息完整时为已支付",
			order: &mall_orders.MallOrders{
				OutTradeNo: "out-001",
				TradeNo:    "trade-001",
				PayTime:    now,
			},
			want: pb.PaymentStatus_PAYMENT_STATUS_PAID,
		},
		{
			name: "只有支付时间时异常",
			order: &mall_orders.MallOrders{
				PayTime: now,
			},
			want: pb.PaymentStatus_PAYMENT_STATUS_ABNORMAL,
		},
		{
			name: "缺少支付宝交易号时异常",
			order: &mall_orders.MallOrders{
				OutTradeNo: "out-002",
				PayTime:    now,
			},
			want: pb.PaymentStatus_PAYMENT_STATUS_ABNORMAL,
		},
		{
			name: "只有交易号时异常",
			order: &mall_orders.MallOrders{
				TradeNo: "trade-003",
			},
			want: pb.PaymentStatus_PAYMENT_STATUS_ABNORMAL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paymentStatusOf(tt.order)
			if got != tt.want {
				t.Fatalf("paymentStatusOf() got = %v, want %v", got, tt.want)
			}
		})
	}

}
