package interceptor

import (
	"context"
	"testing"
	"time"

	"budgetmatch-sim/infra/serviceauth"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestNamedServicePolicyNeverFallsBackToLegacySecret(t *testing.T) {
	const method = "/mall.ProductIndexService/ListProductIndex"
	token, err := serviceauth.GenerateScopedToken(serviceauth.ServiceAgent, serviceauth.ServiceMall,
		serviceauth.PurposeProductIndexRead, testServiceSecret, time.Minute)
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		secrets map[string]string
		allowed bool
	}{
		{"no map", nil, false},
		{"missing named key", map[string]string{"other": testServiceSecret}, false},
		{"empty named key", map[string]string{"index": ""}, false},
		{"wrong named key", map[string]string{"index": "wrong"}, false},
		{"explicit named key", map[string]string{"index": testServiceSecret}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newServiceAuthConfig()
			cfg.ServiceSecrets = tc.secrets
			cfg.NoAuthMethods = map[string]struct{}{method: {}} // Service policy takes precedence.
			cfg.ServiceMethods[method] = ServiceMethodPolicy{
				Caller: serviceauth.ServiceAgent, Audience: serviceauth.ServiceMall,
				SecretName: "index", Purpose: serviceauth.PurposeProductIndexRead,
			}
			called := false
			_, err := UnaryServerInterceptor(cfg)(incomingBearerContext(token), nil, &grpc.UnaryServerInfo{FullMethod: method},
				func(ctx context.Context, _ interface{}) (interface{}, error) {
					called = true
					require.Equal(t, serviceauth.ServiceAgent, ctx.Value(ContextKeyServiceName))
					require.Equal(t, serviceauth.PurposeProductIndexRead, ctx.Value(ContextKeyServicePurpose))
					require.Nil(t, ctx.Value(ContextKeyUserId))
					require.Nil(t, ctx.Value(ContextKeyToken))
					return nil, nil
				})
			require.Equal(t, tc.allowed, called)
			require.Equal(t, tc.allowed, err == nil)
		})
	}
}
