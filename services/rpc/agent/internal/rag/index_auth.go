package rag

import (
	"context"
	"time"

	"budgetmatch-sim/infra/serviceauth"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const indexReadMethod = "/mall.ProductIndexService/ListProductIndex"

// IndexAuthInterceptor is only installed on the dedicated background index client.
// Tokens are minted per RPC (not cached for the duration of a full scan).
// Never attach a service credential to user/product-write/order RPCs.
func IndexAuthInterceptor(secret string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if method != indexReadMethod {
			return status.Error(codes.PermissionDenied, "index client method not allowed")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := serviceauth.ValidateDedicatedSecret(secret); err != nil {
			return status.Error(codes.Unauthenticated, "index service credential not configured")
		}
		token, err := serviceauth.GenerateScopedToken(serviceauth.ServiceAgent, serviceauth.ServiceMall,
			serviceauth.PurposeProductIndexRead, secret, time.Minute)
		if err != nil {
			return status.Error(codes.Unauthenticated, "index service token unavailable")
		}
		md, _ := metadata.FromOutgoingContext(ctx)
		md = md.Copy()
		// Replace, not append: a carried user Token must not precede the service Token.
		md.Set("authorization", "Bearer "+token)
		return invoker(metadata.NewOutgoingContext(ctx, md), method, req, reply, cc, opts...)
	}
}
