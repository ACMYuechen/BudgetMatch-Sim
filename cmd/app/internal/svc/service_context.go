// Code scaffolded by goctl. Safe to edit.

package svc

import (
	"time"

	"budgetmatch-sim/cmd/app/internal/config"
	"budgetmatch-sim/cmd/app/internal/middleware"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/limit"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/client/userservice"
	"budgetmatch-sim/services/rpc/mall/client/orderservice"
	"budgetmatch-sim/services/rpc/mall/client/productservice"
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
	AuthClient        authservice.AuthService
	UserClient        userservice.UserService
	ActivityClient    activityservice.ActivityService
	SkuClient         skuservice.SkuService
	SeckillClient     seckillservice.SeckillService
	MallProductClient productservice.ProductService
	MallOrderClient   orderservice.OrderService
	AgentClient       recommendservice.RecommendService

	// 中间件配置
	AuthMiddleware             rest.Middleware
	SeckillRateLimitMiddleware rest.Middleware
}

func NewServiceContext(c config.Config, redisClient redis.UniversalClient) *ServiceContext {
	valid := validator.New(validator.WithRequiredStructEnabled())

	// token 传播拦截器：将 context 中的 JWT 自动注入 outgoing gRPC metadata
	tokenPropagator := zrpc.WithUnaryClientInterceptor(interceptor.UnaryClientInterceptor())

	authclient := authservice.NewAuthService(zrpc.MustNewClient(c.AuthRpc, tokenPropagator))
	userclient := userservice.NewUserService(zrpc.MustNewClient(c.AuthRpc, tokenPropagator))
	seckillclient := zrpc.MustNewClient(c.SeckillRpc, tokenPropagator)
	activityclient := activityservice.NewActivityService(seckillclient)
	skuclient := skuservice.NewSkuService(seckillclient)
	seckillSvcClient := seckillservice.NewSeckillService(seckillclient)
	mallclient := zrpc.MustNewClient(c.MallRpc, tokenPropagator)
	mallProductClient := productservice.NewProductService(mallclient)
	mallOrderClient := orderservice.NewOrderService(mallclient)
	agentClient := recommendservice.NewRecommendService(zrpc.MustNewClient(c.AgentRpc, tokenPropagator))

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
		AuthClient:        authclient,
		UserClient:        userclient,
		ActivityClient:    activityclient,
		SkuClient:         skuclient,
		SeckillClient:     seckillSvcClient,
		MallProductClient: mallProductClient,
		MallOrderClient:   mallOrderClient,
		AgentClient:       agentClient,

		// 中间件配置
		AuthMiddleware:             middleware.NewAuthMiddleware(authclient).Handle,
		SeckillRateLimitMiddleware: seckillRateMiddleware,
	}
}
