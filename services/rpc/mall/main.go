package main

import (
	"flag"
	"fmt"

	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/internal/config"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
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
	sg.Add(s)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)

	// add RocketMQ producer lifecycle management
	if ctx.RocketMQProducer != nil {
		sg.Add(rocketmq.NewProducerService(ctx.RocketMQProducer))
	}

	// add RocketMQ order event consumer
	sg.Add(mq.NewOrderEventConsumer(ctx.Config.RocketMQ, ctx.SkuStore, ctx.Redis))

	sg.Start()
}
