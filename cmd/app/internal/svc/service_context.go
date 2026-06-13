// Code scaffolded by goctl. Safe to edit.

package svc

import (
	"time"

	"budgetmatch-sim/cmd/app/internal/config"
	"budgetmatch-sim/cmd/app/internal/middleware"
	"budgetmatch-sim/infra/limit"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/client/userservice"
	"budgetmatch-sim/services/rpc/seckill/client/activityservice"
	"budgetmatch-sim/services/rpc/seckill/client/seckillservice"
	"budgetmatch-sim/services/rpc/seckill/client/skuservice"

	"github.com/go-playground/validator/v10"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config    config.Config
	Validator *validator.Validate
	Redis     redis.UniversalClient

	// RPC 客户端
	AuthClient     authservice.AuthService
	UserClient     userservice.UserService
	ActivityClient activityservice.ActivityService
	SkuClient      skuservice.SkuService
	SeckillClient  seckillservice.SeckillService

	// 中间件配置
	AuthMiddleware         rest.Middleware
	SeckillRateLimitMiddleware rest.Middleware
}

func NewServiceContext(c config.Config, redisClient redis.UniversalClient) *ServiceContext {
	valid := validator.New(validator.WithRequiredStructEnabled())

	authclient := authservice.NewAuthService(zrpc.MustNewClient(c.AuthRpc))
	userclient := userservice.NewUserService(zrpc.MustNewClient(c.AuthRpc))
	seckillclient := zrpc.MustNewClient(c.SeckillRpc)
	activityclient := activityservice.NewActivityService(seckillclient)
	skuclient := skuservice.NewSkuService(seckillclient)
	seckillSvcClient := seckillservice.NewSeckillService(seckillclient)

	// 创建秒杀限流中间件（仅对 /token 和 /orders 路径限流，60 秒窗口内最多 10 次请求）
	var seckillRateMiddleware rest.Middleware
	if redisClient != nil {
		windowLimiter := limit.NewSlidingWindowLimiter(redisClient, time.Minute, 10)
		seckillRateMiddleware = middleware.NewSeckillRateLimitMiddleware(windowLimiter).Handle
	}

	return &ServiceContext{
		Config:    c,
		Validator: valid,
		Redis:     redisClient,

		// RPC 客户端
		AuthClient:     authclient,
		UserClient:     userclient,
		ActivityClient: activityclient,
		SkuClient:      skuclient,
		SeckillClient:  seckillSvcClient,

		// 中间件配置
		AuthMiddleware:             middleware.NewAuthMiddleware(authclient).Handle,
		SeckillRateLimitMiddleware: seckillRateMiddleware,
	}
}
