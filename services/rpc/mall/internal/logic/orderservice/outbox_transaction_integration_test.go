package orderservicelogic

import (
	"context"
	stderrors "errors"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
	"budgetmatch-sim/services/rpc/mall/model/products"
	"budgetmatch-sim/services/rpc/mall/pb"
)

func TestCreateOrderRollsBackWhenOutboxInsertFails(t *testing.T) {
	tx, serviceContext := newIntegrationServiceContext(t)
	serviceContext.OrderOutboxStore = &failingOutboxStore{MallOrderOutboxModel: serviceContext.OrderOutboxStore}
	product, sku := seedProductAndSku(t, serviceContext, 5)

	_, err := NewCreateOrderLogic(context.Background(), serviceContext).CreateOrder(&pb.CreateOrderReq{
		UserId: "user-1", SkuId: sku.Id, Quantity: 2, IdempotencyKey: "create-rollback-" + product.Id,
	})
	require.Error(t, err)

	order, findErr := serviceContext.OrderStore.FindByIdempotencyKey(context.Background(), "create-rollback-"+product.Id)
	require.NoError(t, findErr)
	assert.Nil(t, order)
	storedSku, findErr := serviceContext.SkuStore.FindOne(context.Background(), sku.Id)
	require.NoError(t, findErr)
	assert.Equal(t, 5, storedSku.Stock)
	require.NoError(t, tx.Rollback().Error)
}

func TestCancelOrderRollsBackWhenOutboxInsertFails(t *testing.T) {
	tx, serviceContext := newIntegrationServiceContext(t)
	_, sku := seedProductAndSku(t, serviceContext, 5)
	created, err := NewCreateOrderLogic(context.Background(), serviceContext).CreateOrder(&pb.CreateOrderReq{
		UserId: "user-1", SkuId: sku.Id, Quantity: 2, IdempotencyKey: "cancel-rollback-" + sku.Id,
	})
	require.NoError(t, err)
	serviceContext.OrderOutboxStore = &failingOutboxStore{MallOrderOutboxModel: serviceContext.OrderOutboxStore}

	_, err = NewCancelOrderLogic(context.Background(), serviceContext).CancelOrder(&pb.CancelOrderReq{OrderId: created.OrderId, UserId: "user-1"})
	require.Error(t, err)

	order, findErr := serviceContext.OrderStore.FindOne(context.Background(), created.OrderId)
	require.NoError(t, findErr)
	assert.Equal(t, mall_orders.OrderStatusPending, order.Status)
	storedSku, findErr := serviceContext.SkuStore.FindOne(context.Background(), sku.Id)
	require.NoError(t, findErr)
	assert.Equal(t, 3, storedSku.Stock)
	require.NoError(t, tx.Rollback().Error)
}

func TestConfirmPaymentRollsBackWhenOutboxInsertFails(t *testing.T) {
	tx, serviceContext := newIntegrationServiceContext(t)
	_, sku := seedProductAndSku(t, serviceContext, 5)
	created, err := NewCreateOrderLogic(context.Background(), serviceContext).CreateOrder(&pb.CreateOrderReq{
		UserId: "user-1", SkuId: sku.Id, Quantity: 2, IdempotencyKey: "payment-rollback-" + sku.Id,
	})
	require.NoError(t, err)
	serviceContext.OrderOutboxStore = &failingOutboxStore{MallOrderOutboxModel: serviceContext.OrderOutboxStore}

	_, err = NewConfirmPaymentLogic(context.Background(), serviceContext).ConfirmPayment(&pb.ConfirmPaymentReq{
		OrderId:    created.OrderId,
		UserId:     "user-1",
		Amount:     2000,
		OutTradeNo: "out-trade-payment-rollback",
		TradeNo:    "trade-payment-rollback",
	})
	require.Error(t, err)

	order, findErr := serviceContext.OrderStore.FindOne(context.Background(), created.OrderId)
	require.NoError(t, findErr)
	assert.Equal(t, mall_orders.OrderStatusPending, order.Status)
	assert.Empty(t, order.OutTradeNo)
	assert.Empty(t, order.TradeNo)
	assert.True(t, order.PayTime.IsZero())
	require.NoError(t, tx.Rollback().Error)
}

func newIntegrationServiceContext(t *testing.T) (*gorm.DB, *svc.ServiceContext) {
	t.Helper()
	dsn := os.Getenv("BUDGETMATCH_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set BUDGETMATCH_TEST_POSTGRES_DSN to run PostgreSQL transaction integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	for _, table := range []interface{ CreateTable() error }{
		products.NewProductsModel(db),
		product_skus.NewProductSkusModel(db),
		mall_orders.NewMallOrdersModel(db),
		mall_order_items.NewMallOrderItemsModel(db),
		mall_order_outbox.NewMallOrderOutboxModel(db),
	} {
		require.NoError(t, table.CreateTable())
	}
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() {
		if tx.Error == nil {
			_ = tx.Rollback().Error
		}
	})

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	return tx, &svc.ServiceContext{
		DB:               tx,
		Redis:            redisClient,
		ProductStore:     products.NewProductsModel(tx),
		SkuStore:         product_skus.NewProductSkusModel(tx),
		OrderStore:       mall_orders.NewMallOrdersModel(tx),
		OrderItemStore:   mall_order_items.NewMallOrderItemsModel(tx),
		OrderOutboxStore: mall_order_outbox.NewMallOrderOutboxModel(tx),
	}
}

func seedProductAndSku(t *testing.T, serviceContext *svc.ServiceContext, stock int) (*products.Products, *product_skus.ProductSkus) {
	t.Helper()
	product := &products.Products{Id: products.NewProductId(), UserId: "seller-1", Name: "test product", Content: "{}", Status: 1, AgentComment: "{}"}
	require.NoError(t, serviceContext.ProductStore.InsertOne(context.Background(), product))
	sku := &product_skus.ProductSkus{Id: product_skus.NewProductSkuId(), ProductId: product.Id, Name: "test sku", Specs: "{}", Price: 1000, Stock: stock, Status: 1, AgentComment: "{}"}
	require.NoError(t, serviceContext.SkuStore.InsertOne(context.Background(), sku))
	return product, sku
}

type failingOutboxStore struct {
	mall_order_outbox.MallOrderOutboxModel
}

func (f *failingOutboxStore) InsertTx(*gorm.DB, *mall_order_outbox.MallOrderOutbox) error {
	return stderrors.New("forced outbox insert failure")
}
