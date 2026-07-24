package interceptor

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// TokenFromContext 从 context 中提取 JWT token 字符串：
// 优先读取 gRPC 服务端拦截器注入的 ContextKeyToken（contextKey 类型），
// 其次读取 HTTP 中间件注入的 "token"（string 类型），以兼容两种来源。
func TokenFromContext(ctx context.Context) string {
	if tok, ok := ctx.Value(ContextKeyToken).(string); ok && tok != "" {
		return tok
	}
	if tok, ok := ctx.Value("token").(string); ok && tok != "" {
		return tok
	}
	return ""
}

// UnaryClientInterceptor 返回一个 gRPC 客户端拦截器，自动从 context 读取 JWT token
// 并作为 "authorization: Bearer <token>" 注入 outgoing gRPC metadata，
// 使下游 RPC 服务的服务端拦截器可以校验调用方身份。
//
// 若 context 中无 token（如 HandleNotify 等免鉴权回调），则不做注入。
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		token := TokenFromContext(ctx)
		if token != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", fmt.Sprintf("Bearer %s", token))
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
