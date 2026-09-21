package main

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	productservice "budgetmatch-sim/services/rpc/mall/internal/server/productservice"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type onlineCandidateReader struct{ calls atomic.Int32 }

func (r *onlineCandidateReader) FindActiveCandidates(context.Context, []string) ([]product_index.CandidateFacts, error) {
	r.calls.Add(1)
	return []product_index.CandidateFacts{{SkuId: "a", ProductId: "p", Price: 100, Stock: 1}}, nil
}

func (r *onlineCandidateReader) FindActiveDemandCandidates(context.Context, []string) ([]product_index.CandidateFacts, error) {
	r.calls.Add(1)
	return []product_index.CandidateFacts{{SkuId: "a", ProductId: "p", Price: 100, Stock: 1,
		CategoryCode: "keyboard", CategoryTaxonomy: candidatecontract.DemandTaxonomyVersion, CategoryRevision: 1}}, nil
}

func TestCandidateRPCRequiresUserIdentityAndUsesRealHandler(t *testing.T) {
	c := testMallConfig()
	reader := &onlineCandidateReader{}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(interceptor.UnaryServerInterceptor(c.RPCAuthConfig())))
	pb.RegisterProductServiceServer(server, productservice.NewProductServiceServer(&svc.ServiceContext{CandidateStore: reader}))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///candidate-check-test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	user, err := auth.GenerateToken("user", c.JwtAuth.Secret, 3600, role.RoleUser)
	require.NoError(t, err)
	admin, err := auth.GenerateToken("admin", c.JwtAuth.Secret, 3600, role.RoleAdmin)
	require.NoError(t, err)
	index := mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	payment, err := serviceauth.GenerateToken(serviceauth.ServicePayment, serviceauth.ServiceMall, c.ServiceAuth.Secret, time.Minute)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, token string
		allowed     bool
	}{
		{"user", user, true}, {"admin", admin, true}, {"missing", "", false}, {"index", index, false}, {"payment", payment, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, includeCategory := range []bool{false, true} {
				before := reader.calls.Load()
				resp, err := pb.NewProductServiceClient(conn).CheckProductCandidates(bearerCtx(t, tc.token), &pb.CheckProductCandidatesReq{SkuIds: []string{"a", "absent"}, IncludeDemandCategory: includeCategory})
				if tc.allowed {
					require.NoError(t, err)
					require.Equal(t, before+1, reader.calls.Load())
					require.Len(t, resp.Results, 2)
					require.Equal(t, pb.CandidateState_CANDIDATE_STATE_ACTIVE, resp.Results[0].State)
					require.Equal(t, pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE, resp.Results[1].State)
					if includeCategory {
						require.Equal(t, candidatecontract.DemandCategoryContract, resp.DemandCategoryContract)
						require.Equal(t, "keyboard", resp.Results[0].Facts.DemandCategory.Code)
					} else {
						require.Empty(t, resp.DemandCategoryContract)
						require.Nil(t, resp.Results[0].Facts.DemandCategory)
					}
				} else {
					require.Equal(t, codes.Unauthenticated, status.Code(err))
					require.Equal(t, before, reader.calls.Load())
				}
			}
		})
	}
}
