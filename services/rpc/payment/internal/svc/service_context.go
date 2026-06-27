package svc

import (
	"budgetmatch-sim/infra/alipay"
	"budgetmatch-sim/infra/database"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/services/rpc/payment/internal/config"
	"budgetmatch-sim/services/rpc/payment/model/payments"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type ServiceContext struct {
	Config       config.Config
	DB           *gorm.DB
	Redis        redis.UniversalClient
	PaymentStore payments.PaymentsModel
	Alipay       *alipay.Client
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

	// init model
	paymentStore := payments.NewPaymentsModel(db.DB())

	// auto-migrate tables
	if c.Database.AutoMigrate {
		if err := paymentStore.CreateTable(); err != nil {
			logx.Must(err)
		}
	}

	// init alipay client（未配置密钥时返回 nil，服务可启动，发起支付时再报未配置）
	aliClient, err := alipay.NewClient(c.Alipay)
	if err != nil {
		logx.Must(err)
	}
	if aliClient == nil {
		logx.Info("alipay not configured, payment will be unavailable until keys are set")
	}

	return &ServiceContext{
		Config:       c,
		DB:           db.DB(),
		Redis:        redisClient.Client(),
		PaymentStore: paymentStore,
		Alipay:       aliClient,
	}
}
