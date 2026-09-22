package authclient

import (
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/pb"
	"context"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"testing"
)

type authorityStub struct {
	authservice.AuthService
	response *pb.ValidateTokenResp
	err      error
}

func (s authorityStub) ValidateToken(context.Context, *pb.ValidateTokenReq, ...grpc.CallOption) (*pb.ValidateTokenResp, error) {
	return s.response, s.err
}
func TestAuthorityFailsClosed(t *testing.T) {
	token, err := auth.GenerateToken("user", "secret", 3600, 100)
	require.NoError(t, err)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", token))
	for _, tc := range []struct {
		name  string
		stub  authorityStub
		allow bool
	}{
		{"unavailable", authorityStub{err: context.DeadlineExceeded}, false},
		{"nil response", authorityStub{}, false},
		{"nil user", authorityStub{response: &pb.ValidateTokenResp{}}, false},
		{"wrong user", authorityStub{response: &pb.ValidateTokenResp{User: &pb.UserInfo{Id: "other", Role: 100}}}, false},
		{"changed role", authorityStub{response: &pb.ValidateTokenResp{User: &pb.UserInfo{Id: "user", Role: 2}}}, false},
		{"current user", authorityStub{response: &pb.ValidateTokenResp{User: &pb.UserInfo{Id: "user", Role: 100}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			check := interceptor.UnaryServerInterceptor(interceptor.AuthConfig{Secret: "secret", ValidateUser: Validator(tc.stub)})
			_, err := check(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test/User"}, func(context.Context, any) (any, error) { called = true; return nil, nil })
			if tc.allow {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errors.InvalidToken)
			}
			require.Equal(t, tc.allow, called)
		})
	}
}
