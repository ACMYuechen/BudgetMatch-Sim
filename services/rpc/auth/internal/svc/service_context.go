package svc

import (
	"budgetmatch-sim/infra/database"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/services/rpc/auth/internal/config"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/model/user_profile"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type ServiceContext struct {
	Config           config.Config
	DB               *gorm.DB
	Redis            redis.UniversalClient
	UserStore        user.UsersModel
	UserProfileStore user_profile.UserProfilesModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化 PostgreSQL
	db, err := database.NewDatabase(c.Database)
	if err != nil {
		logx.Must(err)
	}

	// 初始化 Redis
	redisClient, err := iredis.NewRedisDB(c.CacheRedis)
	if err != nil {
		logx.Must(err)
	}

	// user 相关 model
	userStore := user.NewUsersModel(db.DB())
	userProfileStore := user_profile.NewUserProfilesModel(db.DB())

	// 自动建表
	if c.Database.AutoMigrate {
		tables := []interface{ CreateTable() error }{
			userStore,
			userProfileStore,
		}
		for _, t := range tables {
			if err := t.CreateTable(); err != nil {
				logx.Must(err)
			}
		}
	}

	return &ServiceContext{
		Config:           c,
		DB:               db.DB(),
		Redis:            redisClient.Client(),
		UserStore:        userStore,
		UserProfileStore: userProfileStore,
	}
}
