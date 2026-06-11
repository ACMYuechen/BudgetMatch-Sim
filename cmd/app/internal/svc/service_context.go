// Code scaffolded by goctl. Safe to edit.

package svc

import (
	"budgetmatch-sim/cmd/app/internal/config"
	"budgetmatch-sim/cmd/app/internal/middleware"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/client/userservice"

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
	AuthClient authservice.AuthService
	UserClient userservice.UserService

	// 中间件配置
	AuthMiddleware rest.Middleware
}

func NewServiceContext(c config.Config, redisClient redis.UniversalClient) *ServiceContext {
	valid := validator.New(validator.WithRequiredStructEnabled())

	authclient := authservice.NewAuthService(zrpc.MustNewClient(c.AuthRpc))
	userclient := userservice.NewUserService(zrpc.MustNewClient(c.AuthRpc))

	return &ServiceContext{
		Config:    c,
		Validator: valid,
		Redis:     redisClient,

		// RPC 客户端
		AuthClient: authclient,
		UserClient: userclient,

		// 中间件配置
		AuthMiddleware: middleware.NewAuthMiddleware(authclient).Handle,
	}
}
