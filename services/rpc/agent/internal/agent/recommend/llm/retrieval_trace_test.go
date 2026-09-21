package llm

import (
	"context"
	"encoding/json"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Exercises the optional traced-provider contract without external retrieval.
type tracedProducts struct {
	calls int
	err   error
}

func (*tracedProducts) Name() string { return "rag.hybrid_rrf" }
func (*tracedProducts) SearchProducts(context.Context, tools.SearchProductsReq) ([]tools.ProductCandidate, error) {
	panic("traced capability was bypassed")
}
func (p *tracedProducts) SearchProductsWithTrace(context.Context, tools.SearchProductsReq) (tools.ProductSearch, error) {
	p.calls++
	return tools.ProductSearch{
		Candidates: []tools.ProductCandidate{{Id: "a", Name: "keyboard", PriceCents: 100, Stock: 2,
			Evidence: agentcore.CandidateEvidence{Source: agentcore.RetrievalMallKeyword, ProductID: "p",
				Ranking: agentcore.CandidateRanking{Method: "hybrid_rrf_v1", KeywordRank: 1, FusionScore: 1.0 / 61}}}},
		Calls: []agentcore.ToolCall{
			{Name: "retrieval.keyword", Success: true, Detail: "loaded 1 candidates"},
			{Name: "retrieval.vector", Success: false, Detail: "error_code=rpc_unavailable duration_ms=1"},
			{Name: "PRIVATE_UPSTREAM_LABEL", Success: false, Detail: "PRIVATE_QUERY_AND_RAW_ERROR"},
		},
	}, p.err
}

func TestRetrievalTraceSurvivesReactAndDeterministicSelection(t *testing.T) {
	for _, mode := range []string{"no_tools", "search_only", "search_and_select"} {
		t.Run(mode, func(t *testing.T) {
			var responses []*schema.Message
			if mode != "no_tools" {
				responses = append(responses, toolCallMessage("search", toolSearchProducts, searchArgs{Query: "keyboard"}))
			}
			if mode == "search_and_select" {
				responses = append(responses, toolCallMessage("select", toolSelectBundle, selectArgs{}))
			}
			responses = append(responses, schema.AssistantMessage("done", nil))
			provider := &tracedProducts{}
			runner := NewAgent(&scriptedModel{responses: responses}, provider, selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{}).WithMaxStep(8)
			result, err := runner.Run(context.Background(), agentcore.Input{Query: "keyboard", BudgetCents: 1000, MaxItems: 2})
			require.NoError(t, err)
			require.Equal(t, 1, provider.calls)
			require.Len(t, result.Items, 1)
			require.Len(t, result.Candidates, 1)
			require.Equal(t, "hybrid_rrf_v1", result.Candidates[0].Evidence.Ranking.Method)
			counts := make(map[string]int)
			for _, call := range result.ToolsUsed {
				counts[call.Name]++
			}
			require.Equal(t, 1, counts["retrieval.keyword"])
			require.Equal(t, 1, counts["retrieval.vector"])
			if mode != "search_and_select" {
				require.Equal(t, 1, counts["selector.fallback"])
			}
			data, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(data), "PRIVATE")
			require.NotContains(t, string(data), "hybrid_rrf_v1", "ranking evidence stays internal")
		})
	}
}

func TestFailedTracedSearchRecordsSafeDiagnosticsWithoutCachingCandidates(t *testing.T) {
	provider := &tracedProducts{err: status.Error(codes.Unavailable, "PRIVATE_UPSTREAM_ERROR")}
	s := newSession(provider, selector.NewBundleSelector(), agentcore.Intent{BudgetCents: 1000, MaxItems: 2})
	result, err := s.searchProducts(context.Background(), searchArgs{Query: "keyboard"})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Nil(t, result)
	require.False(t, s.hasCandidates())
	_, _, calls := s.snapshot()
	require.Len(t, calls, 3)
	data, err := json.Marshal(calls)
	require.NoError(t, err)
	require.NotContains(t, string(data), "PRIVATE")
}
