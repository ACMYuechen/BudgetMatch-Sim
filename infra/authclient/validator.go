// Package authclient connects business RPC authentication to the account authority.
package authclient

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/zrpc"
)

// WithAuthority requires an Auth RPC client; missing configuration fails at
// startup, and an unavailable authority never falls back to token-only access.
func WithAuthority(cfg interceptor.AuthConfig, clientConfig zrpc.RpcClientConf) interceptor.AuthConfig {
	client := authservice.NewAuthService(zrpc.MustNewClient(clientConfig))
	cfg.ValidateUser = Validator(client)
	return cfg
}

func Validator(client authservice.AuthService) func(context.Context, string) (string, int64, error) {
	return func(ctx context.Context, token string) (string, int64, error) {
		resp, err := client.ValidateToken(ctx, &pb.ValidateTokenReq{Token: token})
		if err != nil {
			return "", 0, errors.InvalidToken
		}
		if resp == nil || resp.User == nil || resp.User.Id == "" {
			return "", 0, errors.InvalidToken
		}
		return resp.User.Id, int64(resp.User.Role), nil
	}
}
