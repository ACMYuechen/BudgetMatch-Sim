package svc

import (
	"time"

	"budgetmatch-sim/infra/database"
	"budgetmatch-sim/infra/limit"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/services/rpc/seckill/internal/config"
	"budgetmatch-sim/services/rpc/seckill/internal/consumer"
	"budgetmatch-sim/services/rpc/seckill/internal/stock"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_activity"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_order"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_sku"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type ServiceContext struct {
	Config        config.Config
	DB            *gorm.DB
	Redis         redis.UniversalClient
	ActivityStore seckill_activity.SeckillActivitiesModel
	SkuStore      seckill_sku.SeckillSkusModel
	OrderStore    seckill_order.SeckillOrdersModel
	StockManager  *stock.StockManager
	OrderConsumer *consumer.OrderConsumer

	// 限流器
	ActivityRateLimiter limit.Limiter // 活动级全局滑动窗口
	UserRateLimiter     limit.Limiter // 用户级令牌桶
}

func NewServiceContext(c config.Config) *ServiceContext {
	// init PostgreSQL
	db, err := database.NewDatabase(c.Database)
	if err != nil {
		logx.Must(err)
	}

	// init Redis
	redisClient, err := iredis.NewRedisDB(c.CacheRedis)
	if err != nil {
		logx.Must(err)
	}

	// init models
	activityStore := seckill_activity.NewSeckillActivitiesModel(db.DB())
	skuStore := seckill_sku.NewSeckillSkusModel(db.DB())
	orderStore := seckill_order.NewSeckillOrdersModel(db.DB())

	// auto-migrate tables
	if c.Database.AutoMigrate {
		tables := []interface{ CreateTable() error }{
			activityStore,
			skuStore,
			orderStore,
		}
		for _, t := range tables {
			if err := t.CreateTable(); err != nil {
				logx.Must(err)
			}
		}
	}

	// init stock manager
	stockManager := stock.NewStockManager(redisClient.Client())

	// 活动级全局滑动窗口：5 秒窗口内最多 1000 次请求
	activityRateLimiter := limit.NewSlidingWindowLimiter(redisClient.Client(), 5*time.Second, 1000)
	// 用户级令牌桶：容量 5，每 60 秒补充 1 个令牌
	userRateLimiter := limit.NewTokenBucketLimiter(redisClient.Client(), 5, 1, 60*time.Second)

	// init order consumer
	orderConsumer := consumer.NewOrderConsumer(redisClient.Client(), db.DB(), orderStore, skuStore, stockManager)

	return &ServiceContext{
		Config:        c,
		DB:            db.DB(),
		Redis:         redisClient.Client(),
		ActivityStore: activityStore,
		SkuStore:      skuStore,
		OrderStore:    orderStore,

		StockManager:  stockManager,
		OrderConsumer: orderConsumer,

		ActivityRateLimiter: activityRateLimiter,
		UserRateLimiter:     userRateLimiter,
	}
}
