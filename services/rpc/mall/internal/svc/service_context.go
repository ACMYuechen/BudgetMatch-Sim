package svc

import (
	"budgetmatch-sim/infra/database"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/services/rpc/mall/internal/config"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_event_inbox"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
	"budgetmatch-sim/services/rpc/mall/model/products"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type ServiceContext struct {
	Config           config.Config
	DB               *gorm.DB
	Redis            redis.UniversalClient
	ProductStore     products.ProductsModel
	SkuStore         product_skus.ProductSkusModel
	OrderStore       mall_orders.MallOrdersModel
	OrderItemStore   mall_order_items.MallOrderItemsModel
	OrderOutboxStore mall_order_outbox.MallOrderOutboxModel
	OrderInboxStore  mall_order_event_inbox.MallOrderEventInboxModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	db, err := database.NewDatabase(c.Database)
	if err != nil {
		logx.Must(err)
	}

	redisClient, err := iredis.NewRedisDB(c.CacheRedis)
	if err != nil {
		logx.Must(err)
	}

	productStore := products.NewProductsModel(db.DB())
	skuStore := product_skus.NewProductSkusModel(db.DB())
	orderStore := mall_orders.NewMallOrdersModel(db.DB())
	orderItemStore := mall_order_items.NewMallOrderItemsModel(db.DB())
	orderOutboxStore := mall_order_outbox.NewMallOrderOutboxModel(db.DB())
	orderInboxStore := mall_order_event_inbox.NewMallOrderEventInboxModel(db.DB())

	if c.Database.AutoMigrate {
		tables := []interface{ CreateTable() error }{
			productStore,
			skuStore,
			orderStore,
			orderItemStore,
			orderOutboxStore,
			orderInboxStore,
		}
		for _, t := range tables {
			if err := t.CreateTable(); err != nil {
				logx.Must(err)
			}
		}
	}

	return &ServiceContext{
		Config: c,
		DB:     db.DB(),
		Redis:  redisClient.Client(),

		ProductStore:     productStore,
		SkuStore:         skuStore,
		OrderStore:       orderStore,
		OrderItemStore:   orderItemStore,
		OrderOutboxStore: orderOutboxStore,
		OrderInboxStore:  orderInboxStore,
	}
}
