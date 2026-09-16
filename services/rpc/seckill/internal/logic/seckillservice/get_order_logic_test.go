package seckillservicelogic

import (
	"context"
	"testing"
	"time"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_order"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type orderReadStore struct {
	seckill_order.SeckillOrdersModel
	order *seckill_order.SeckillOrders
	err   error
	calls int
}

func (s *orderReadStore) FindOne(_ context.Context, _ string) (*seckill_order.SeckillOrders, error) {
	s.calls++
	return s.order, s.err
}

func TestGetSeckillOrderOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, user string
		missing    bool
		storeErr   error
		want       error
	}{
		{"anonymous", "", false, nil, errors.Unauthorized},
		{"other user", "user-2", false, nil, errors.SeckillOrderNotFound},
		{"missing", "user-1", true, nil, errors.SeckillOrderNotFound},
		{"database failure", "user-1", false, errors.Database, errors.Database},
		{"owner", "user-1", false, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, tc.user)
			store := &orderReadStore{order: &seckill_order.SeckillOrders{Id: "order-1", UserId: "user-1", ActivityId: "activity-1", SkuId: "sku-1", Quantity: 2, TotalAmount: 39800, Status: 1, CreatedAt: time.UnixMilli(1789516800000)}, err: tc.storeErr}
			if tc.missing {
				store.order = nil
			}
			result, err := NewGetOrderLogic(ctx, &svc.ServiceContext{OrderStore: store}).GetOrder(&pb.GetOrderReq{OrderId: "order-1"})
			if err != tc.want {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want != nil && result != nil {
				t.Fatal("must not return order data on failure")
			}
			if tc.user == "" && store.calls != 0 {
				t.Fatal("anonymous request reached store")
			}
			if tc.want == nil && (result.UserId != "user-1" || result.Quantity != 2 || result.TotalAmount != 39800 || result.Status != 1 || result.CreatedAt != 1789516800000) {
				t.Fatalf("incorrect order mapping: %+v", result)
			}
		})
	}
}
