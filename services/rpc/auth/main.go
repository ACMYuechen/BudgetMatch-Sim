package main

import (
	"flag"
	"fmt"

	"budgetmatch-sim/services/rpc/auth/internal/config"
	"budgetmatch-sim/services/rpc/auth/internal/interceptor"
	infrainterceptor "budgetmatch-sim/infra/interceptor"
	authservice "budgetmatch-sim/services/rpc/auth/internal/server/authservice"
	userservice "budgetmatch-sim/services/rpc/auth/internal/server/userservice"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/config.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	ctx := svc.NewServiceContext(c)

	// 创建服务组
	sg := service.NewServiceGroup()
	defer sg.Stop()

	// 初始化 RPC 服务，注册 AuthService 和 UserService
	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterAuthServiceServer(grpcServer, authservice.NewAuthServiceServer(ctx))
		pb.RegisterUserServiceServer(grpcServer, userservice.NewUserServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	// 添加请求日志拦截器（最外层）和认证拦截器
	s.AddUnaryInterceptors(
		infrainterceptor.LoggingInterceptor(c.JwtAuth.Secret),
		interceptor.AuthInterceptor(ctx),
	)
	sg.Add(s)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)

	sg.Start()
}
