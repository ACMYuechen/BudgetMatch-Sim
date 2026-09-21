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

const indexScanMethod = "/mall.ProductIndexService/ScanProductIndex"

// IndexAuthInterceptor is only installed on the dedicated background index client.
// Tokens are minted per RPC (not cached for the duration of a full scan).
// Never attach a service credential to user/product-write/order RPCs.
func IndexAuthInterceptor(secret string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if method != indexReadMethod {
			return status.Error(codes.PermissionDenied, "index client method not allowed")
		}
		ctx, err := indexAuthContext(ctx, secret)
		if err != nil {
			return err
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// IndexStreamAuthInterceptor only authorizes the snapshot RPC, never user or
// payment streams. Tokens are checked at admission; the RPC has a shorter bound.
func IndexStreamAuthInterceptor(secret string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if method != indexScanMethod {
			return nil, status.Error(codes.PermissionDenied, "index client method not allowed")
		}
		ctx, err := indexAuthContext(ctx, secret)
		if err != nil {
			return nil, err
		}
		return streamer(ctx, desc, cc, method, opts...)
	}
}

func indexAuthContext(ctx context.Context, secret string) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := serviceauth.ValidateDedicatedSecret(secret); err != nil {
		return nil, status.Error(codes.Unauthenticated, "index service credential not configured")
	}
	token, err := serviceauth.GenerateScopedToken(serviceauth.ServiceAgent, serviceauth.ServiceMall,
		serviceauth.PurposeProductIndexRead, secret, time.Minute)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "index service token unavailable")
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	// Replace, not append: a carried user token must not precede the service token.
	md.Set("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(ctx, md), nil
}
