package interceptor

import (
	"context"
	"errors"
	"testing"
	"time"

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
