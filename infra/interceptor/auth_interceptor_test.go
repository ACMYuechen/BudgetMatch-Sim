package interceptor

import (
	userauth "budgetmatch-sim/infra/auth"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/infra/serviceauth"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	testUserSecret    = "user-secret-for-interceptor-testing"
	testServiceSecret = "service-secret-for-interceptor-testing"
	confirmMethod     = "/mall.OrderService/ConfirmPayment"
)

// newServiceAuthConfig 返回测试使用的认证策略
func newServiceAuthConfig() AuthConfig {
	return AuthConfig{
		Secret:        testUserSecret,
		ServiceSecret: testServiceSecret,
		ServiceMethods: map[string]ServiceMethodPolicy{
			confirmMethod: {
				Caller:   serviceauth.ServicePayment,
				Audience: serviceauth.ServiceMall,
			},
		},
	}
}

// incomingBearerContext 构造携带 Bearer Token 的 gRPC 入站 Context
func incomingBearerContext(token string) context.Context {
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer "+token),
	)
}

// TestServiceMethodAcceptsPaymentToken 验证合法 payment-rpc Token 可以调用 ConfirmPayment
func TestServiceMethodAcceptsPaymentToken(t *testing.T) {
	token, err := serviceauth.GenerateToken(serviceauth.ServicePayment, serviceauth.ServiceMall, testServiceSecret, time.Minute)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true

		caller, ok := ctx.Value(ContextKeyServiceName).(string)
		require.True(t, ok)
		assert.Equal(t, serviceauth.ServicePayment, caller)
		return "ok", nil
	}

	authInterceptor := UnaryServerInterceptor(newServiceAuthConfig())
	resp, err := authInterceptor(
		incomingBearerContext(token), "request", &grpc.UnaryServerInfo{FullMethod: confirmMethod}, handler,
	)

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "ok", resp)
}

// TestServiceMethodRejectsUserToken 验证普通用户 JWT 不能调用 ConfirmPayment
func TestServiceMethodRejectsUserToken(t *testing.T) {
	token, err := userauth.GenerateToken("user-1", testUserSecret, 3600, role.RoleUser)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "unexpected", nil
	}

	authInterceptor := UnaryServerInterceptor(newServiceAuthConfig())
	resp, err := authInterceptor(
		incomingBearerContext(token), "request", &grpc.UnaryServerInfo{FullMethod: confirmMethod}, handler,
	)

	assert.ErrorIs(t, err, apperrors.InvalidToken)
	assert.False(t, called)
	assert.Nil(t, resp)
}

// TestServiceMethodRejectsInvalidServiceToken 验证错误服务身份均被拒绝
func TestServiceMethodRejectsInvalidServiceToken(t *testing.T) {
	wrongCallerToken, err := serviceauth.GenerateToken("agent-rpc", serviceauth.ServiceMall, testServiceSecret, time.Minute)
	require.NoError(t, err)

	wrongAudienceToken, err := serviceauth.GenerateToken(serviceauth.ServicePayment, "agent-rpc", testServiceSecret, time.Minute)
	require.NoError(t, err)

	wrongSecretToken, err := serviceauth.GenerateToken(serviceauth.ServicePayment, serviceauth.ServiceMall, "wrong-service-secret", time.Minute)
	require.NoError(t, err)

	tests := []struct {
		name  string
		token string
	}{
		{name: "wrong caller", token: wrongCallerToken},
		{name: "wrong audience", token: wrongAudienceToken},
		{name: "wrong secret", token: wrongSecretToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				called = true
				return nil, nil
			}

			authInterceptor := UnaryServerInterceptor(newServiceAuthConfig())
			resp, err := authInterceptor(
				incomingBearerContext(tt.token), "request", &grpc.UnaryServerInfo{FullMethod: confirmMethod}, handler,
			)

			assert.ErrorIs(t, err, apperrors.InvalidToken)
			assert.False(t, called)
			assert.Nil(t, resp)
		})
	}
}

// TestRegularMethodsStillAcceptUserToken 验证普通 RPC 的用户认证没有受到影响
func TestRegularMethodsStillAcceptUserToken(t *testing.T) {
	token, err := userauth.GenerateToken("user-1", testUserSecret, 3600, role.RoleUser)
	require.NoError(t, err)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		userId, ok := ctx.Value(ContextKeyUserId).(string)
		require.True(t, ok)
		assert.Equal(t, "user-1", userId)

		return "ok", nil
	}

	authInterceptor := UnaryServerInterceptor(newServiceAuthConfig())
	resp, err := authInterceptor(
		incomingBearerContext(token), "request", &grpc.UnaryServerInfo{FullMethod: "/mall.OrderService/GetOrder"}, handler,
	)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
}
