package rag

import (
	"context"
	"strings"
	"testing"
	"time"

	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const indexTestSecret = "agent-index-unit-test-independent-secret"

func TestIndexClientMintsScopedTokenPerCall(t *testing.T) {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyToken, "user-jwt")
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer old-user-token", "x-trace", "trace"))
	var ids []string
	invoke := func(ctx context.Context, method string, _, _ interface{}, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		require.Equal(t, indexReadMethod, method)
		md, _ := metadata.FromOutgoingContext(ctx)
		require.Equal(t, []string{"trace"}, md.Get("x-trace"))
		require.Len(t, md.Get("authorization"), 1)
		token := strings.TrimPrefix(md.Get("authorization")[0], "Bearer ")
		claims, err := serviceauth.ValidateScopedToken(token, indexTestSecret, serviceauth.ServiceAgent,
			serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
		require.NoError(t, err)
		require.Equal(t, time.Minute, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
		ids = append(ids, claims.ID)
		return nil
	}
	for range 2 {
		require.NoError(t, IndexAuthInterceptor(indexTestSecret)(ctx, indexReadMethod, nil, nil, nil, invoke))
	}
	require.NotEqual(t, ids[0], ids[1])
	original, _ := metadata.FromOutgoingContext(ctx)
	require.Equal(t, []string{"Bearer old-user-token"}, original.Get("authorization"))
	require.NoError(t, IndexAuthInterceptor(indexTestSecret)(context.Background(), indexReadMethod, nil, nil, nil,
		func(ctx context.Context, _ string, _, _ interface{}, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, _ := metadata.FromOutgoingContext(ctx)
			require.Len(t, md.Get("authorization"), 1)
			return nil
		}))
}

func TestIndexClientFailsClosed(t *testing.T) {
	calls := 0
	invoke := func(context.Context, string, interface{}, interface{}, *grpc.ClientConn, ...grpc.CallOption) error {
		calls++
		return nil
	}
	for _, method := range []string{"/mall.ProductService/ListProducts", "/mall.ProductService/CreateSku", "/mall.OrderService/ConfirmPayment", "/mall.OrderService/GetOrder", ""} {
		err := IndexAuthInterceptor(indexTestSecret)(context.Background(), method, nil, nil, nil, invoke)
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	}
	for _, secret := range []string{"", "short"} {
		err := IndexAuthInterceptor(secret)(context.Background(), indexReadMethod, nil, nil, nil, invoke)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := IndexAuthInterceptor(indexTestSecret)(ctx, indexReadMethod, nil, nil, nil, invoke)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls)
}

func TestOnlineClientStillForwardsUserToken(t *testing.T) {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyToken, "user-jwt")
	require.NoError(t, interceptor.UnaryClientInterceptor()(ctx, "/mall.ProductService/ListProducts", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ interface{}, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, _ := metadata.FromOutgoingContext(ctx)
			require.Equal(t, []string{"Bearer user-jwt"}, md.Get("authorization"))
			return nil
		}))
}

func TestIndexStreamClientMintsIsolatedTokenAndFailsClosed(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer user-token", "x-trace", "trace"))
	var ids []string
	streamer := func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		require.Equal(t, indexScanMethod, method)
		md, _ := metadata.FromOutgoingContext(ctx)
		require.Equal(t, []string{"trace"}, md.Get("x-trace"))
		require.Len(t, md.Get("authorization"), 1)
		claims, err := serviceauth.ValidateScopedToken(strings.TrimPrefix(md.Get("authorization")[0], "Bearer "), indexTestSecret,
			serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
		require.NoError(t, err)
		require.Equal(t, time.Minute, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
		ids = append(ids, claims.ID)
		return nil, nil
	}
	for range 2 {
		_, err := IndexStreamAuthInterceptor(indexTestSecret)(ctx, nil, nil, indexScanMethod, streamer)
		require.NoError(t, err)
	}
	require.NotEqual(t, ids[0], ids[1])
	original, _ := metadata.FromOutgoingContext(ctx)
	require.Equal(t, []string{"Bearer user-token"}, original.Get("authorization"))
	for _, method := range []string{indexReadMethod, "/mall.ProductService/ListProducts", "/mall.OrderService/ConfirmPayment", ""} {
		_, err := IndexStreamAuthInterceptor(indexTestSecret)(ctx, nil, nil, method, streamer)
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	}
	for _, secret := range []string{"", "short"} {
		_, err := IndexStreamAuthInterceptor(secret)(ctx, nil, nil, indexScanMethod, streamer)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err := IndexStreamAuthInterceptor(indexTestSecret)(ctx, nil, nil, indexScanMethod, streamer)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, ids, 2, "rejected requests must not reach transport")
}
