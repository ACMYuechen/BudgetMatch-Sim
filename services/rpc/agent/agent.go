// agent 是 Agent RPC 服务的主入口，负责加载配置并启动 gRPC 服务器。
package main

import (
	"flag"
	"fmt"

	"budgetmatch-sim/services/rpc/agent/internal/config"
	recommendserviceServer "budgetmatch-sim/services/rpc/agent/internal/server/recommendservice"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// configFile 指向配置文件路径，默认值为 "etc/agent.yaml"。
var configFile = flag.String("f", "etc/agent.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterRecommendServiceServer(grpcServer, recommendserviceServer.NewRecommendServiceServer(ctx))

		// 仅在开发或测试模式下注册 gRPC 反射服务，便于调试工具（如 grpcurl）发现接口。
		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
