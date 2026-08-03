package main

import (
	"flag"
	"fmt"

	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/payment/internal/config"
	paymentserviceServer "budgetmatch-sim/services/rpc/payment/internal/server/paymentservice"
	"budgetmatch-sim/services/rpc/payment/internal/svc"
	"budgetmatch-sim/services/rpc/payment/pb"

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

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterPaymentServiceServer(grpcServer, paymentserviceServer.NewPaymentServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	// 注册请求日志拦截器（最外层）和认证拦截器
	s.AddUnaryInterceptors(
		interceptor.LoggingInterceptor(c.JwtAuth.Secret),
		interceptor.UnaryServerInterceptor(interceptor.AuthConfig{
			Secret: c.JwtAuth.Secret,
			NoAuthMethods: map[string]struct{}{
				"/payment.PaymentService/HandleNotify": {}, // 支付宝异步回调
			},
		}),
	)
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
