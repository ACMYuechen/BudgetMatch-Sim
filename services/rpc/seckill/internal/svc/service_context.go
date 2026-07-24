package svc

import (
	"time"

	"budgetmatch-sim/infra/configcenter"
	"budgetmatch-sim/infra/database"
	"budgetmatch-sim/infra/dlock"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/limit"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/services/rpc/mall/client/productservice"
	"budgetmatch-sim/services/rpc/seckill/internal/config"
	"budgetmatch-sim/services/rpc/seckill/internal/consumer"
	"budgetmatch-sim/services/rpc/seckill/internal/stock"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_activity"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_order"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_sku"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
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

	// 商城商品 RPC 客户端（创建秒杀 SKU 时预加载商品快照）
	MallProductClient productservice.ProductService

	// etcd 分布式锁管理器
	LockManager *dlock.Manager

	// 动态配置中心
	ConfigCenter *configcenter.ConfigCenter

	// 运行时动态配置状态（封装了读写锁）
	DynamicState *dynamicState

	// 限流器
	ActivityRateLimiter *limit.SlidingWindowLimiter // 活动级全局滑动窗口
	UserRateLimiter     *limit.TokenBucketLimiter   // 用户级令牌桶
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

	// init etcd lock manager
	lockManager, err := dlock.NewManager(c.Etcd.Hosts)
	if err != nil {
		logx.Must(err)
	}

	// init mall product client（NonBlock：mall 启动晚于 seckill 时不阻塞，调用时再解析连接）
	mallProductClient := productservice.NewProductService(zrpc.MustNewClient(c.MallRpc,
		zrpc.WithUnaryClientInterceptor(interceptor.UnaryClientInterceptor())))

	svc := &ServiceContext{
		Config:        c,
		DB:            db.DB(),
		Redis:         redisClient.Client(),
		ActivityStore: activityStore,
		SkuStore:      skuStore,
		OrderStore:    orderStore,

		StockManager:      stockManager,
		OrderConsumer:     orderConsumer,
		MallProductClient: mallProductClient,
		LockManager:       lockManager,
		DynamicState:      newDynamicState(),

		ActivityRateLimiter: activityRateLimiter,
		UserRateLimiter:     userRateLimiter,
	}

	// 启动 etcd 动态配置监听
	svc.ConfigCenter, err = configcenter.New(c.Etcd.Hosts)
	if err != nil {
		logx.Must(err)
	}
	if err := svc.ConfigCenter.Watch(seckillConfigKey, svc.loadDynamicConfig); err != nil {
		logx.Must(err)
	}

	return svc
}
