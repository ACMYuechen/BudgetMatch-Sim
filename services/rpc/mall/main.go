package main

import (
	"flag"
	"fmt"

	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/internal/config"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/outbox"
	orderservice "budgetmatch-sim/services/rpc/mall/internal/server/orderservice"
	productservice "budgetmatch-sim/services/rpc/mall/internal/server/productservice"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"

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

	sg := service.NewServiceGroup()
	defer sg.Stop()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterProductServiceServer(grpcServer, productservice.NewProductServiceServer(ctx))
		pb.RegisterOrderServiceServer(grpcServer, orderservice.NewOrderServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)

	// 注册请求日志拦截器（最外层）和认证拦截器
	s.AddUnaryInterceptors(
		interceptor.LoggingInterceptor(c.JwtAuth.Secret),
		interceptor.UnaryServerInterceptor(interceptor.AuthConfig{
		Secret: c.JwtAuth.Secret,
		AdminMethods: map[string]struct{}{
			"/mall.ProductService/CreateProduct": {},
			"/mall.ProductService/UpdateProduct": {},
			"/mall.ProductService/DeleteProduct": {},
			"/mall.ProductService/CreateSku":     {},
			"/mall.ProductService/UpdateSku":     {},
			"/mall.ProductService/DeleteSku":     {},
		},
	}))

	sg.Add(s)

	// add RocketMQ producer lifecycle management
	if ctx.RocketMQProducer != nil {
		sg.Add(rocketmq.NewProducerService(ctx.RocketMQProducer))
		sg.Add(outbox.NewDispatcher(ctx.OrderOutboxStore, ctx.RocketMQProducer, outbox.DefaultConfig()))
	}

	// add RocketMQ order event consumer
	sg.Add(mq.NewOrderEventConsumer(ctx.Config.RocketMQ, ctx.SkuStore, ctx.Redis))

	sg.Start()
}
