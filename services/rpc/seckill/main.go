package main

import (
	"flag"
	"fmt"

	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/seckill/internal/config"
	activityservice "budgetmatch-sim/services/rpc/seckill/internal/server/activityservice"
	seckillservice "budgetmatch-sim/services/rpc/seckill/internal/server/seckillservice"
	skuservice "budgetmatch-sim/services/rpc/seckill/internal/server/skuservice"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"

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

	// create service group
	sg := service.NewServiceGroup()
	defer sg.Stop()

	// init RPC server, register three services
	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterActivityServiceServer(grpcServer, activityservice.NewActivityServiceServer(ctx))
		pb.RegisterSkuServiceServer(grpcServer, skuservice.NewSkuServiceServer(ctx))
		pb.RegisterSeckillServiceServer(grpcServer, seckillservice.NewSeckillServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	// 注册请求日志拦截器（最外层）和认证拦截器
	s.AddUnaryInterceptors(
		interceptor.LoggingInterceptor(c.JwtAuth.Secret),
		interceptor.UnaryServerInterceptor(interceptor.AuthConfig{
		Secret: c.JwtAuth.Secret,
		AdminMethods: map[string]struct{}{
			"/seckill.ActivityService/CreateActivity":  {},
			"/seckill.ActivityService/UpdateActivity":  {},
			"/seckill.ActivityService/GetActivity":     {},
			"/seckill.ActivityService/ListActivities":  {},
			"/seckill.ActivityService/DeleteActivity":  {},
			"/seckill.ActivityService/PreheatActivity": {},
			"/seckill.ActivityService/OnlineActivity":  {},
			"/seckill.ActivityService/OfflineActivity": {},
			"/seckill.SkuService/CreateSku":            {},
			"/seckill.SkuService/UpdateSku":            {},
			"/seckill.SkuService/GetSku":               {},
			"/seckill.SkuService/ListSkusByActivity":   {},
			"/seckill.SkuService/DeleteSku":            {},
		},
	}))

	sg.Add(s)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)

	// add order consumer as a service
	sg.Add(ctx.OrderConsumer)

	sg.Start()
}
