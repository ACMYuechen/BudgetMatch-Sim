package svc

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type demandMallServer struct {
	pb.UnimplementedProductServiceServer
	calls  atomic.Int32
	checks atomic.Int32
	legacy atomic.Bool
}

func (s *demandMallServer) ListProducts(context.Context, *pb.ListProductsReq) (*pb.ListProductsResp, error) {
	s.calls.Add(1)
	return &pb.ListProductsResp{List: []*pb.Product{{Id: "p", Name: "keyboard", Status: 1}}, Total: 1}, nil
}

func (s *demandMallServer) ListSkusByProduct(context.Context, *pb.ListSkusByProductReq) (*pb.ListSkusByProductResp, error) {
	s.calls.Add(1)
	return &pb.ListSkusByProductResp{List: []*pb.Sku{{Id: "a", ProductId: "p", Name: "stale", Price: 100, Stock: 1, Status: 1}}, Total: 1}, nil
}

func (s *demandMallServer) CheckProductCandidates(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
	s.calls.Add(1)
	s.checks.Add(1)
	if !req.IncludeDemandCategory || len(req.SkuIds) != 1 || req.SkuIds[0] != "a" {
		return nil, status.Error(codes.InvalidArgument, "classification opt-in required")
	}
	r := &pb.CheckProductCandidatesResp{CheckedAtUnixMs: time.Now().UnixMilli(), DemandCategoryContract: candidatecontract.DemandCategoryContract,
		Results: []*pb.CandidateCheck{{SkuId: "a", State: pb.CandidateState_CANDIDATE_STATE_ACTIVE,
			Facts: &pb.CandidateFacts{SkuId: "a", ProductId: "p", ProductName: "checked keyboard", Price: 125, Stock: 2,
				DemandCategory: &pb.DemandCategoryFact{Code: "keyboard", TaxonomyVersion: candidatecontract.DemandTaxonomyVersion, Revision: 1}}}}}
	if s.legacy.Load() {
		r.DemandCategoryContract = ""
	}
	return r, nil
}

func TestMallDemandWiringForwardsUserJWTAndPersistsOnlyVerifiedResults(t *testing.T) {
	const secret = "local-test-only-demand-jwt-secret"
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(interceptor.UnaryServerInterceptor(interceptor.AuthConfig{Secret: secret})))
	mall := &demandMallServer{}
	pb.RegisterProductServiceServer(server, mall)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///mall-demand-test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithUnaryInterceptor(interceptor.UnaryClientInterceptor()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := pb.NewProductServiceClient(conn)
	executor, err := newDemandExecutor("mall", client)
	require.NoError(t, err)
	mem := memory.NewInMemory(memory.Conf{})
	service := recommendagent.NewService(nil, nil, mem).WithDemandExecutor(executor)
	_, err = service.PlanDemand(context.Background(), agent.Input{UserId: "u", ConversationId: "c", TurnId: "plan", Query: "键盘", BudgetCents: 1000, MaxItems: 1},
		`{"schema_version":1,"required":{"operation":"replace","values":["keyboard"]}}`)
	require.NoError(t, err)
	_, err = service.ExecuteDemand(context.Background(), "u", "c", "plan", "missing-token")
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Zero(t, mall.calls.Load())
	_, found, err := mem.FindTurn(context.Background(), "u", "c", "missing-token")
	require.NoError(t, err)
	require.False(t, found)
	token, err := auth.GenerateToken("u", secret, 3600, role.RoleUser)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyToken, token)
	result, err := service.ExecuteDemand(ctx, "u", "c", "plan", "execute")
	require.NoError(t, err)
	require.Equal(t, "complete", result.Status)
	require.EqualValues(t, 125, result.TotalPriceCents)
	require.Equal(t, "mall_checked_snapshot_only", result.Execution.Scope)
	require.Equal(t, int32(2), mall.checks.Load())
	before := mall.calls.Load()
	replay, err := service.ExecuteDemand(ctx, "u", "c", "plan", "execute")
	require.NoError(t, err)
	wantJSON, _ := json.Marshal(result)
	gotJSON, _ := json.Marshal(replay)
	require.JSONEq(t, string(wantJSON), string(gotJSON))
	require.Equal(t, before, mall.calls.Load(), "history replay is not a fresh Mall check")
	_, err = service.ExecuteDemand(ctx, "other", "c", "plan", "execute")
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, before, mall.calls.Load())
	mall.legacy.Store(true)
	_, err = service.ExecuteDemand(ctx, "u", "c", "plan", "old-mall")
	require.Equal(t, codes.Unavailable, status.Code(err))
	_, found, err = mem.FindTurn(ctx, "u", "c", "old-mall")
	require.NoError(t, err)
	require.False(t, found)
	for _, mode := range []string{"", "disabled"} {
		e, err := newDemandExecutor(mode, client)
		require.NoError(t, err)
		require.Nil(t, e)
	}
	_, err = newDemandExecutor("demo", client)
	require.Error(t, err)
	_, err = newDemandExecutor("mall", nil)
	require.Error(t, err)
}
