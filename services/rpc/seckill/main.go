package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/gateway"

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

	sg.Add(s)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)

	// start gateway if configured
	if c.Gateway.Port > 0 {
		gw := gateway.MustNewServer(c.Gateway, gateway.WithHeaderProcessor(forwardHeadersToMetadata))
		sg.Add(gw)
		fmt.Printf("Starting gateway server at %s:%d...\n", c.Gateway.Host, c.Gateway.Port)
	}

	// add order consumer as a service
	sg.Add(ctx.OrderConsumer)

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
