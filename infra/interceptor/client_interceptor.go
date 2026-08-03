package interceptor

import (
	"context"

	"budgetmatch-sim/infra/request"

	"google.golang.org/grpc"
)

// UnaryClientInterceptor 返回一个 gRPC 客户端拦截器，自动将统一请求上下文中的
// 白名单字段注入 outgoing gRPC metadata，使下游服务可以继续读取请求信息。
//
// 若 Context 中无可透传字段，则保持原 Context 不变。
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		ctx = request.NewOutgoingContext(ctx)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
