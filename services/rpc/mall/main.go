package main

import (
	"budgetmatch-sim/infra/serviceauth"
	"flag"
	"fmt"
	"time"

	"budgetmatch-sim/infra/interceptor"
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
			Secret:        c.JwtAuth.Secret,
			ServiceSecret: c.ServiceAuth.Secret,
			ServiceMethods: map[string]interceptor.ServiceMethodPolicy{
				"/mall.OrderService/ConfirmPayment": {
					Caller:   serviceauth.ServicePayment,
					Audience: serviceauth.ServiceMall,
				},
			},
			AdminMethods: map[string]struct{}{
				"/mall.ProductService/CreateProduct":     {},
				"/mall.ProductService/UpdateProduct":     {},
				"/mall.ProductService/DeleteProduct":     {},
				"/mall.ProductService/CreateSku":         {},
				"/mall.ProductService/UpdateSku":         {},
				"/mall.ProductService/DeleteSku":         {},
				"/mall.OrderService/GetOrderOutboxStats": {},
				"/mall.OrderService/ListOrderOutbox":     {},
				"/mall.OrderService/GetOrderOutbox":      {},
				"/mall.OrderService/ReplayOrderOutbox":   {},
			},
		}))

	sg.Add(s)
	sg.Add(outbox.NewMetricsCollector(ctx.OrderOutboxStore, 15*time.Second))

	// The dispatcher owns the producer and reconnects without blocking RPC startup.
	if len(ctx.Config.RocketMQ.NameServers) > 0 {
		sg.Add(outbox.NewResilientDispatcher(ctx.OrderOutboxStore, ctx.Config.RocketMQ, outbox.DefaultConfig()))
	}

	// add RocketMQ order event consumer
	sg.Add(mq.NewOrderEventConsumer(ctx.Config.RocketMQ, ctx.SkuStore, ctx.Redis, ctx.OrderInboxStore))

	sg.Start()
}
