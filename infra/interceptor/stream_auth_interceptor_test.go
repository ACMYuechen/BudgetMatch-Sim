package interceptor

import (
	"context"
	"errors"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/infra/serviceauth"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type authTestStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent bool
}

func (s *authTestStream) Context() context.Context  { return s.ctx }
func (s *authTestStream) SendMsg(interface{}) error { s.sent = true; return nil }

func TestStreamAuthUsesNamedPolicyAndPreservesTransport(t *testing.T) {
	const method = "/mall.ProductIndexService/ScanProductIndex"
	token, err := serviceauth.GenerateScopedToken(serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead, testServiceSecret, time.Minute)
	require.NoError(t, err)
	for _, allowed := range []bool{false, true} {
		cfg := newServiceAuthConfig()
		cfg.NoAuthMethods = map[string]struct{}{method: {}}
		cfg.ServiceMethods[method] = ServiceMethodPolicy{Caller: serviceauth.ServiceAgent, Audience: serviceauth.ServiceMall, SecretName: "index", Purpose: serviceauth.PurposeProductIndexRead}
		if allowed {
			cfg.ServiceSecrets = map[string]string{"index": testServiceSecret}
		}
		ctx, cancel := context.WithTimeout(incomingBearerContext(token), time.Second)
		stream := &authTestStream{ctx: ctx}
		called := false
		handlerErr := errors.New("handler error")
		err := StreamServerInterceptor(cfg)(nil, stream, &grpc.StreamServerInfo{FullMethod: method, IsServerStream: true}, func(_ interface{}, s grpc.ServerStream) error {
			called = true
			require.Equal(t, serviceauth.ServiceAgent, s.Context().Value(ContextKeyServiceName))
			require.Equal(t, serviceauth.PurposeProductIndexRead, s.Context().Value(ContextKeyServicePurpose))
			require.Nil(t, s.Context().Value(ContextKeyToken))
			require.Nil(t, s.Context().Value(ContextKeyUserId))
			deadline, _ := ctx.Deadline()
			actual, _ := s.Context().Deadline()
			require.Equal(t, deadline, actual)
			require.NoError(t, s.SendMsg(nil))
			cancel()
			require.ErrorIs(t, s.Context().Err(), context.Canceled)
			return handlerErr
		})
		cancel()
		require.Equal(t, allowed, called)
		require.Equal(t, allowed, stream.sent)
		if allowed {
			require.ErrorIs(t, err, handlerErr)
		} else {
			require.Error(t, err)
		}
	}
}

func TestStreamDeadlinePolicyRunsBeforeHandler(t *testing.T) {
	const method = "/mall.ProductIndexService/ScanProductIndex"
	token, err := serviceauth.GenerateScopedToken(serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead, testServiceSecret, time.Minute)
	require.NoError(t, err)
	cfg := AuthConfig{ServiceSecrets: map[string]string{"index": testServiceSecret}, ServiceMethods: map[string]ServiceMethodPolicy{
		method: {Caller: serviceauth.ServiceAgent, Audience: serviceauth.ServiceMall, SecretName: "index", Purpose: serviceauth.PurposeProductIndexRead, MaxStreamDuration: time.Second},
	}}
	for _, tc := range []struct {
		name     string
		duration time.Duration
		cancel   bool
		code     codes.Code
	}{
		{"missing", 0, false, codes.InvalidArgument}, {"long", time.Minute, false, codes.InvalidArgument},
		{"canceled", time.Second, true, codes.Canceled}, {"expired", -time.Second, false, codes.DeadlineExceeded},
		{"bounded", time.Second, false, codes.OK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := incomingBearerContext(token)
			var cancel context.CancelFunc
			if tc.duration != 0 {
				ctx, cancel = context.WithTimeout(ctx, tc.duration)
				defer cancel()
			}
			if tc.cancel {
				cancel()
			}
			called := false
			err := StreamServerInterceptor(cfg)(nil, &authTestStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: method}, func(interface{}, grpc.ServerStream) error { called = true; return nil })
			require.Equal(t, tc.code, status.Code(err))
			require.Equal(t, tc.code == codes.OK, called)
		})
	}
}

func TestUserStreamDeadlinePreservesIdentityAndCancellation(t *testing.T) {
	const method = "/agent.RecommendService/RecommendStream"
	token, err := auth.GenerateToken("trusted", testUserSecret, 3600, role.RoleUser)
	require.NoError(t, err)
	cfg := AuthConfig{Secret: testUserSecret, StreamMaxDurations: map[string]time.Duration{method: time.Second}}
	for _, duration := range []time.Duration{0, time.Minute, -time.Second, time.Second} {
		ctx := incomingBearerContext(token)
		cancel := func() {}
		if duration != 0 {
			ctx, cancel = context.WithTimeout(ctx, duration)
		}
		called := false
		err := StreamServerInterceptor(cfg)(nil, &authTestStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: method}, func(_ interface{}, stream grpc.ServerStream) error {
			called = true
			require.Equal(t, "trusted", stream.Context().Value(ContextKeyUserId))
			require.Equal(t, token, stream.Context().Value(ContextKeyToken))
			require.Nil(t, stream.Context().Value(ContextKeyServiceName))
			deadline, _ := ctx.Deadline()
			actual, _ := stream.Context().Deadline()
			require.Equal(t, deadline, actual)
			cancel()
			require.ErrorIs(t, stream.Context().Err(), context.Canceled)
			return nil
		})
		cancel()
		if duration == time.Second {
			require.NoError(t, err)
			require.True(t, called)
		} else {
			require.Error(t, err)
			require.False(t, called)
		}
	}
	// A stream-only deadline policy does not change unary admission.
	_, err = UnaryServerInterceptor(cfg)(incomingBearerContext(token), nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.NoError(t, err)
}

func TestStreamDeadlineUsesStricterServiceOrMethodLimit(t *testing.T) {
	const method = "/mall.ProductIndexService/ScanProductIndex"
	token, err := serviceauth.GenerateToken(serviceauth.ServiceAgent, serviceauth.ServiceMall, testServiceSecret, time.Minute)
	require.NoError(t, err)
	for _, limits := range [][2]time.Duration{{time.Second, time.Minute}, {time.Minute, time.Second}} {
		cfg := AuthConfig{ServiceSecret: testServiceSecret,
			ServiceMethods:     map[string]ServiceMethodPolicy{method: {Caller: serviceauth.ServiceAgent, Audience: serviceauth.ServiceMall, MaxStreamDuration: limits[0]}},
			StreamMaxDurations: map[string]time.Duration{method: limits[1]}}
		ctx, cancel := context.WithTimeout(incomingBearerContext(token), 10*time.Second)
		called := false
		err := StreamServerInterceptor(cfg)(nil, &authTestStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: method}, func(interface{}, grpc.ServerStream) error { called = true; return nil })
		cancel()
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.False(t, called)
	}
}
