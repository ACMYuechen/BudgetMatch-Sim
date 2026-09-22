package orderservicelogic

import (
	"context"
	"testing"
	"time"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func orderUserContext(id string, role int64) context.Context {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, id)
	return context.WithValue(ctx, interceptor.ContextKeyRole, role)
}

type accessOrderStore struct {
	mall_orders.MallOrdersModel
	order  *mall_orders.MallOrders
	filter mall_orders.MallOrdersListReq
}

func (s *accessOrderStore) FindOne(context.Context, string) (*mall_orders.MallOrders, error) {
	return s.order, nil
}
func (s *accessOrderStore) FindByIdempotencyKey(_ context.Context, key string) (*mall_orders.MallOrders, error) {
	if s.order != nil && s.order.IdempotencyKey == key {
		return s.order, nil
	}
	return nil, nil
}
func (s *accessOrderStore) List(_ context.Context, filter mall_orders.MallOrdersListReq) ([]mall_orders.MallOrders, int64, error) {
	s.filter = filter
	return nil, 0, nil
}

type accessItemStore struct {
	mall_order_items.MallOrderItemsModel
	items []mall_order_items.MallOrderItems
}

func (s *accessItemStore) FindByOrderId(context.Context, string) ([]mall_order_items.MallOrderItems, error) {
	return s.items, nil
}

func TestOrderOwnership(t *testing.T) {
	store := &accessOrderStore{order: &mall_orders.MallOrders{Id: "order", UserId: "victim"}}
	service := &svc.ServiceContext{OrderStore: store, OrderItemStore: &accessItemStore{}}
	for _, requested := range []string{"", "attacker", "victim"} {
		ctx := orderUserContext("attacker", 100)
		_, err := NewGetOrderLogic(ctx, service).GetOrder(&pb.GetOrderReq{OrderId: "order", UserId: requested})
		require.Error(t, err)
		_, err = NewCancelOrderLogic(ctx, service).CancelOrder(&pb.CancelOrderReq{OrderId: "order", UserId: requested})
		require.Error(t, err)
	}
	for _, ctx := range []context.Context{orderUserContext("victim", 100), orderUserContext("admin", 2)} {
		resp, err := NewGetOrderLogic(ctx, service).GetOrder(&pb.GetOrderReq{OrderId: "order"})
		require.NoError(t, err)
		require.Equal(t, "victim", resp.Order.UserId)
	}
	_, err := NewGetOrderLogic(context.Background(), service).GetOrder(&pb.GetOrderReq{OrderId: "order"})
	require.ErrorIs(t, err, errors.Unauthorized)
	for _, tc := range []struct {
		role            int64
		requested, want string
		deny            bool
	}{
		{100, "", "caller", false}, {100, "caller", "caller", false}, {100, "victim", "", true},
		{2, "", "", false}, {2, "victim", "victim", false},
	} {
		_, err := NewListOrdersLogic(orderUserContext("caller", tc.role), service).ListOrders(&pb.ListOrdersReq{UserId: tc.requested})
		if tc.deny {
			require.ErrorIs(t, err, errors.Unauthorized)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.want, store.filter.UserId)
		}
	}
}

func TestOrderMutationsRejectUntrustedIdentityBeforeIO(t *testing.T) {
	for _, ctx := range []context.Context{context.Background(), orderUserContext("attacker", 100), orderUserContext("admin", 2)} {
		_, err := NewCreateOrderLogic(ctx, &svc.ServiceContext{}).CreateOrder(&pb.CreateOrderReq{UserId: "victim", SkuId: "sku", IdempotencyKey: "key"})
		require.ErrorIs(t, err, errors.Unauthorized)
		_, err = NewCancelOrderLogic(ctx, &svc.ServiceContext{}).CancelOrder(&pb.CancelOrderReq{UserId: "victim", OrderId: "order"})
		require.ErrorIs(t, err, errors.Unauthorized)
	}
	for _, ctx := range []context.Context{context.Background(), orderUserContext("user", 100)} {
		_, err := NewUpdateOrderStatusLogic(ctx, &svc.ServiceContext{}).UpdateOrderStatus(&pb.UpdateOrderStatusReq{OrderId: "order", Status: pb.OrderStatus_ORDER_STATUS_SHIPPED})
		require.ErrorIs(t, err, errors.Unauthorized)
	}
	_, err := NewUpdateOrderStatusLogic(orderUserContext("admin", 2), &svc.ServiceContext{}).UpdateOrderStatus(&pb.UpdateOrderStatusReq{OrderId: "order", Status: pb.OrderStatus_ORDER_STATUS_PAID})
	require.ErrorIs(t, err, errors.MallInvalidOrderTransition)
	require.False(t, isValidOrderTransition(mall_orders.OrderStatusPending, mall_orders.OrderStatusPaid))
	require.True(t, isValidOrderTransition(mall_orders.OrderStatusPending, mall_orders.OrderStatusCancelled))
	require.True(t, isValidOrderTransition(mall_orders.OrderStatusPaid, mall_orders.OrderStatusShipped))
}

func TestOrderReplayChecksOwnerAndRequest(t *testing.T) {
	server := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = r.Close() })
	ctx := orderUserContext("owner", 100)
	key := scopedIdempotencyKey("owner", "key")
	store := &accessOrderStore{order: &mall_orders.MallOrders{Id: "order", UserId: "owner", IdempotencyKey: key, Status: mall_orders.OrderStatusShipped}}
	items := &accessItemStore{items: []mall_order_items.MallOrderItems{{SkuId: "sku", Quantity: 2}}}
	service := &svc.ServiceContext{Redis: r, OrderStore: store, OrderItemStore: items}
	req := &pb.CreateOrderReq{SkuId: "sku", Quantity: 2, IdempotencyKey: "key"}
	for _, cached := range []bool{false, true} {
		if cached {
			require.NoError(t, r.Set(ctx, idempotencyKey(key), "order", time.Hour).Err())
		}
		resp, err := NewCreateOrderLogic(ctx, service).CreateOrder(req)
		require.NoError(t, err)
		require.Equal(t, pb.OrderStatus_ORDER_STATUS_SHIPPED, resp.Status)
		req.Quantity = 3
		_, err = NewCreateOrderLogic(ctx, service).CreateOrder(req)
		require.ErrorIs(t, err, errors.Invalid)
		req.Quantity = 2
	}
	store.order.UserId = "victim"
	_, err := NewCreateOrderLogic(ctx, service).CreateOrder(req)
	require.ErrorIs(t, err, errors.MallOrderNotFound)
	require.NotEqual(t, key, scopedIdempotencyKey("victim", "key"))
	require.NotEqual(t, scopedIdempotencyKey("a:b", "c"), scopedIdempotencyKey("a", "b:c"))
	// Legacy retries use a database lookup with an explicit owner check.
	require.NoError(t, r.FlushDB(ctx).Err())
	store.order.UserId = "owner"
	store.order.IdempotencyKey = "key"
	resp, err := NewCreateOrderLogic(ctx, service).CreateOrder(req)
	require.NoError(t, err)
	require.Equal(t, "order", resp.OrderId)
}

func TestOrderCreationIsolatedByUserInDatabase(t *testing.T) {
	_, service := newIntegrationServiceContext(t)
	_, sku := seedProductAndSku(t, service, 10)
	first, err := NewCreateOrderLogic(orderUserContext("one", 100), service).CreateOrder(&pb.CreateOrderReq{SkuId: sku.Id, Quantity: 1, IdempotencyKey: "same"})
	require.NoError(t, err)
	second, err := NewCreateOrderLogic(orderUserContext("two", 100), service).CreateOrder(&pb.CreateOrderReq{SkuId: sku.Id, Quantity: 1, IdempotencyKey: "same"})
	require.NoError(t, err)
	require.NotEqual(t, first.OrderId, second.OrderId)
	require.NoError(t, service.Redis.FlushDB(context.Background()).Err())
	replay, err := NewCreateOrderLogic(orderUserContext("one", 100), service).CreateOrder(&pb.CreateOrderReq{SkuId: sku.Id, Quantity: 1, IdempotencyKey: "same"})
	require.NoError(t, err)
	require.Equal(t, first.OrderId, replay.OrderId)
	_, err = NewCancelOrderLogic(orderUserContext("one", 100), service).CancelOrder(&pb.CancelOrderReq{OrderId: first.OrderId})
	require.NoError(t, err)
}

func TestAdminOperationsPreservePaymentAuthority(t *testing.T) {
	_, service := newIntegrationServiceContext(t)
	_, sku := seedProductAndSku(t, service, 10)
	order, err := NewCreateOrderLogic(orderUserContext("buyer", 100), service).CreateOrder(&pb.CreateOrderReq{SkuId: sku.Id, Quantity: 1, IdempotencyKey: "payment"})
	require.NoError(t, err)
	admin := NewUpdateOrderStatusLogic(orderUserContext("admin", 2), service)
	_, err = admin.UpdateOrderStatus(&pb.UpdateOrderStatusReq{OrderId: order.OrderId, Status: pb.OrderStatus_ORDER_STATUS_PAID})
	require.ErrorIs(t, err, errors.MallInvalidOrderTransition)
	paymentCtx := context.WithValue(context.Background(), interceptor.ContextKeyServiceName, serviceauth.ServicePayment)
	_, err = NewConfirmPaymentLogic(paymentCtx, service).ConfirmPayment(&pb.ConfirmPaymentReq{OrderId: order.OrderId, UserId: "buyer", Amount: 1000, OutTradeNo: "out-trade", TradeNo: "trade"})
	require.NoError(t, err)
	_, err = admin.UpdateOrderStatus(&pb.UpdateOrderStatusReq{OrderId: order.OrderId, Status: pb.OrderStatus_ORDER_STATUS_SHIPPED})
	require.NoError(t, err)
	stored, err := service.OrderStore.FindOne(context.Background(), order.OrderId)
	require.NoError(t, err)
	require.Equal(t, mall_orders.OrderStatusShipped, stored.Status)
	require.Equal(t, "trade", stored.TradeNo)
}
