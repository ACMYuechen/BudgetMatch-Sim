// Code scaffolded by goctl. Safe to edit.

package svc

import (
	"budgetmatch-sim/cmd/admin/internal/config"
	"budgetmatch-sim/cmd/admin/internal/middleware"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/client/userservice"
	"budgetmatch-sim/services/rpc/seckill/client/activityservice"
	"budgetmatch-sim/services/rpc/seckill/client/skuservice"
	"budgetmatch-sim/services/rpc/seckill/client/seckillservice"
	"budgetmatch-sim/services/rpc/mall/client/orderservice"
	"budgetmatch-sim/services/rpc/mall/client/productservice"

	"github.com/go-playground/validator/v10"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config    config.Config
	Validator *validator.Validate

	// RPC 客户端
	AuthClient     authservice.AuthService
	UserClient     userservice.UserService
	ActivityClient activityservice.ActivityService
	SkuClient      skuservice.SkuService
	SeckillClient   seckillservice.SeckillService
	MallProductClient productservice.ProductService
	MallOrderClient   orderservice.OrderService

	// 中间件配置
	AuthMiddleware rest.Middleware
}

func NewServiceContext(c config.Config) *ServiceContext {
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

	return &ServiceContext{
		Config:    c,
		Validator: valid,

		// RPC 客户端
		AuthClient:     authclient,
		UserClient:     userclient,
		ActivityClient: activityclient,
		SkuClient:      skuclient,
		SeckillClient:   seckillSvcClient,
		MallProductClient: mallProductClient,
		MallOrderClient:   mallOrderClient,

		// 中间件配置
		AuthMiddleware: middleware.NewAuthMiddleware(authclient).Handle,
	}
}
