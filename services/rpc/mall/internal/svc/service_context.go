package svc

import (
	"budgetmatch-sim/infra/database"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/internal/config"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
	"budgetmatch-sim/services/rpc/mall/model/products"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type ServiceContext struct {
	Config             config.Config
	DB                 *gorm.DB
	Redis              redis.UniversalClient
	RocketMQProducer   rocketmq.Producer
	OrderEventProducer *mq.OrderEventProducer
	ProductStore       products.ProductsModel
	SkuStore           product_skus.ProductSkusModel
	OrderStore         mall_orders.MallOrdersModel
	OrderItemStore     mall_order_items.MallOrderItemsModel
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

	if c.Database.AutoMigrate {
		tables := []interface{ CreateTable() error }{
			productStore,
			skuStore,
			orderStore,
			orderItemStore,
		}
		for _, t := range tables {
			if err := t.CreateTable(); err != nil {
				logx.Must(err)
			}
		}
	}

	var producer rocketmq.Producer
	var orderEventProducer *mq.OrderEventProducer
	if len(c.RocketMQ.NameServers) > 0 {
		producer, err = rocketmq.NewProducer(c.RocketMQ)
		if err != nil {
			logx.Must(err)
		}
		orderEventProducer = mq.NewOrderEventProducer(producer)
	}

	return &ServiceContext{
		Config: c,
		DB:     db.DB(),
		Redis:  redisClient.Client(),

		RocketMQProducer:   producer,
		OrderEventProducer: orderEventProducer,

		ProductStore:   productStore,
		SkuStore:       skuStore,
		OrderStore:     orderStore,
		OrderItemStore: orderItemStore,
	}
}
