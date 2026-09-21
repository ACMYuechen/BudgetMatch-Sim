package tools_test

import (
	"context"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type integrationKeyword struct{}

func (integrationKeyword) SearchRankedProducts(context.Context, tools.SearchProductsReq, int) ([]tools.ProductCandidate, error) {
	return []tools.ProductCandidate{{Id: "a", Name: "a", Source: "mall", PriceCents: 100, Stock: 1,
		Evidence: agentcore.CandidateEvidence{Source: agentcore.RetrievalMallKeyword, ProductID: "p-a"}}}, nil
}

type integrationVector struct{}

func (integrationVector) Retrieve(context.Context, string, ...retriever.Option) ([]*schema.Document, error) {
	return []*schema.Document{rag.NewCandidateDocument("b", "synthetic", rag.CandidateMetadata{ProductId: "p-b", Name: "b", PriceCents: 200, Stock: 1}).WithScore(.8)}, nil
}

type integrationChecker struct{ calls int }

func (c *integrationChecker) CheckProductCandidates(_ context.Context, req *pb.CheckProductCandidatesReq, _ ...grpc.CallOption) (*pb.CheckProductCandidatesResp, error) {
	c.calls++
	resp := &pb.CheckProductCandidatesResp{CheckedAtUnixMs: time.Now().UnixMilli()}
	for _, id := range req.SkuIds {
		price := int64(400)
		if id == "a" {
			price = 2000
		}
		resp.Results = append(resp.Results, &pb.CandidateCheck{SkuId: id, State: pb.CandidateState_CANDIDATE_STATE_ACTIVE,
			Facts: &pb.CandidateFacts{SkuId: id, ProductId: "p-" + id, ProductName: id, Price: price, Stock: 1}})
	}
	return resp, nil
}

func TestHybridStillRequiresFinalLiveCheckAndReplaysWithoutRPC(t *testing.T) {
	provider, err := tools.NewHybridProductProvider(integrationVector{}, integrationKeyword{}, rag.Config{TopK: 2,
		Retrieval: rag.RetrievalConfig{Strategy: rag.StrategyHybridRRF, InitialK: 2, MaxK: 4}})
	require.NoError(t, err)
	checker := &integrationChecker{}
	service := recommendagent.NewService(recommendagent.NewAgent(provider, selector.NewBundleSelector()), nil, memory.NewInMemory(memory.Conf{})).
		WithFinalizer(recommendagent.NewCandidateFinalizer(tools.NewMallCandidateVerifier(checker)))
	input := agentcore.Input{Query: "keyboard", BudgetCents: 1000, MaxItems: 2, UserId: "u", ConversationId: "c", TurnId: "t"}
	result, err := service.Recommend(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "b", result.Items[0].Id)
	require.EqualValues(t, 400, result.TotalPriceCents)
	require.Equal(t, "retrieval.keyword", result.ToolsUsed[1].Name)
	require.Equal(t, "retrieval.vector", result.ToolsUsed[2].Name)
	require.Equal(t, "candidate.verify", result.ToolsUsed[len(result.ToolsUsed)-1].Name)
	for _, candidate := range result.Candidates {
		require.Equal(t, "hybrid_rrf_v1", candidate.Evidence.Ranking.Method)
		require.Equal(t, agentcore.VerificationChecked, candidate.Evidence.State)
	}
	replayed, err := service.Recommend(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, result.Items, replayed.Items)
	require.Equal(t, result.ToolsUsed, replayed.ToolsUsed)
	require.Equal(t, 1, checker.calls)
}
