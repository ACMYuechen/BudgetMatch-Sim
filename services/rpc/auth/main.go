package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/gateway"

	"budgetmatch-sim/services/rpc/auth/internal/config"
	"budgetmatch-sim/services/rpc/auth/internal/interceptor"
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

	// 添加认证拦截器
	s.AddUnaryInterceptors(interceptor.AuthInterceptor(ctx))
	sg.Add(s)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)

	// 启动 gRPC 网关
	if c.Gateway.Port > 0 {
		gw := gateway.MustNewServer(c.Gateway, gateway.WithHeaderProcessor(forwardHeadersToMetadata))
		sg.Add(gw)
		fmt.Printf("Starting gateway server at %s:%d...\n", c.Gateway.Host, c.Gateway.Port)
	}

	sg.Start()
}

func forwardHeadersToMetadata(header http.Header) []string {
	keys := []string{
		"authorization",
		"x-forwarded-for",
		"x-real-ip",
		"user-agent",
	}

	var vals []string
	for _, key := range keys {
		for _, value := range header.Values(key) {
			if value == "" {
				continue
			}
			vals = append(vals, strings.ToLower(key)+":"+value)
		}
	}

	return vals
}
