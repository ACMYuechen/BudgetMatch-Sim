package interceptor

import (
	"context"
	"testing"

	"budgetmatch-sim/infra/auth"
	infraerrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const interceptorTestSecret = "interceptor-test-secret"

func TestUnaryServerInterceptor(t *testing.T) {
	token, err := auth.GenerateToken("user-1", interceptorTestSecret, 3600, 101)
	require.NoError(t, err)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "bEaReR "+token,
		"x-request-id", "request-1",
	))
	interceptor := UnaryServerInterceptor(AuthConfig{Secret: interceptorTestSecret})
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, err = interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		assert.Equal(t, "user-1", request.UserID(ctx))
		assert.Equal(t, int64(101), request.Role(ctx))
		assert.Equal(t, token, request.Token(ctx))
		assert.Equal(t, "request-1", request.RequestID(ctx))
		return nil, nil
	})
	require.NoError(t, err)
}

func TestUnaryServerInterceptorInvalidToken(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer invalid-token",
	))
	interceptor := UnaryServerInterceptor(AuthConfig{Secret: interceptorTestSecret})
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, err := interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	})
	assert.Equal(t, infraerrors.InvalidToken, err)
}

func TestUnaryServerInterceptorNoAuthMethod(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-request-id", "request-1",
	))
	interceptor := UnaryServerInterceptor(AuthConfig{
		Secret: interceptorTestSecret,
		NoAuthMethods: map[string]struct{}{
			"/test.Service/Public": {},
		},
	})
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Public"}

	_, err := interceptor(ctx, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
		assert.Equal(t, "request-1", request.RequestID(ctx))
		assert.Empty(t, request.Token(ctx))
		return nil, nil
	})
	require.NoError(t, err)
}

func TestUnaryClientInterceptor(t *testing.T) {
	ctx := request.NewContext(context.Background(), &request.Request{
		Token:     "token-1",
		RequestID: "request-1",
	})

	err := UnaryClientInterceptor()(ctx, "/test.Service/Method", nil, nil, nil,
		func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn,
			opts ...grpc.CallOption,
		) error {
			md, ok := metadata.FromOutgoingContext(ctx)
			require.True(t, ok)
			assert.Equal(t, []string{"Bearer token-1"}, md.Get("authorization"))
			assert.Equal(t, []string{"request-1"}, md.Get("x-request-id"))
			return nil
		})
	require.NoError(t, err)
}
