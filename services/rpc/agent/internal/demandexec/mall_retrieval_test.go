package demandexec

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type demandVectorFunc func(context.Context, string, ...retriever.Option) ([]*schema.Document, error)

func (f demandVectorFunc) Retrieve(ctx context.Context, q string, options ...retriever.Option) ([]*schema.Document, error) {
	return f(ctx, q, options...)
}

type demandRetrievalFunc func(context.Context, tools.SearchProductsReq) ([]agent.ProductCandidate, error)

func (demandRetrievalFunc) Name() string { return "test-retrieval" }
func (f demandRetrievalFunc) SearchProducts(ctx context.Context, req tools.SearchProductsReq) ([]agent.ProductCandidate, error) {
	return f(ctx, req)
}

func TestMallHybridRankingReachesBeamButNeverCertifiesCategory(t *testing.T) {
	for _, mode := range []string{"unchanged", "category withdrawn", "price raised"} {
		t.Run(mode, func(t *testing.T) {
			vector := demandVectorFunc(func(_ context.Context, q string, _ ...retriever.Option) ([]*schema.Document, error) {
				require.Contains(t, q, "键盘")
				var docs []*schema.Document
				// Both lanes agree on b: it outranks cheaper a when coverage ties.
				for _, id := range []string{"b", "c", "a"} {
					docs = append(docs, rag.NewCandidateDocument(id, "synthetic", rag.CandidateMetadata{ProductId: "p-" + id,
						Name: "untrusted title", Category: "phone", PriceCents: 1, Stock: 1}).WithScore(.8))
				}
				return docs, nil
			})
			// Put b first in keyword too, making the ranking comparison strict.
			keyword := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) {
				v := mallRaw()
				return []agent.ProductCandidate{v[1], v[2], v[0]}, nil
			})
			provider, err := tools.NewHybridProductProvider(vector, keyword, rag.Config{TopK: 3, Retrieval: rag.RetrievalConfig{Strategy: rag.StrategyHybridRRF, InitialK: 3, MaxK: 3}})
			require.NoError(t, err)
			checks := 0
			client := checkClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
				checks++
				r := mallFacts(req)
				if checks == 2 {
					for _, item := range r.Results {
						if item.SkuId == "b" {
							if mode == "category withdrawn" {
								item.Facts.DemandCategory.Code = "unknown"
								item.Facts.DemandCategory.Revision++
							}
							if mode == "price raised" {
								item.Facts.Price = 300
							}
						}
					}
				}
				return r, nil
			})
			e, err := NewMallRetrieval(provider, client)
			require.NoError(t, err)
			result, err := e.Run(context.Background(), mallIntent())
			require.NoError(t, err)
			require.Equal(t, "complete", result.Status)
			require.Equal(t, 2, checks)
			require.Equal(t, "demand.mall_search", result.ToolsUsed[0].Name, "retain public tool name for rollback readers")
			if mode == "unchanged" {
				require.EqualValues(t, 180, result.TotalPriceCents)
			} else {
				require.EqualValues(t, 150, result.TotalPriceCents)
			}
			for _, c := range result.Candidates {
				require.NotEqual(t, "phone", c.Category)
				require.Equal(t, agent.HybridRankingMethod, c.Evidence.Ranking.Method)
				require.Positive(t, c.Evidence.Ranking.FusionScore)
			}
		})
	}
}

func TestMallRetrievalVectorEvidenceIsOptInAndBounded(t *testing.T) {
	for _, mode := range []string{"vector", "demo", "unknown", "oversize", "wrong parent", "forged category", "invalid rank"} {
		t.Run(mode, func(t *testing.T) {
			checks := 0
			provider := demandRetrievalFunc(func(context.Context, tools.SearchProductsReq) ([]agent.ProductCandidate, error) {
				v := mallRaw()
				for i := range v {
					v[i].Evidence.Source = agent.RetrievalMallVector
				}
				switch mode {
				case "demo":
					v[0].Evidence.State = agent.VerificationDemo
				case "unknown":
					v[0].Evidence.Source = agent.RetrievalUnknown
				case "oversize":
					return make([]agent.ProductCandidate, 33), nil
				case "wrong parent":
					v[0].Evidence.ProductID = "other"
				case "forged category":
					v[0].Evidence.DemandCategory = agent.DemandCategoryEvidence{Code: "phone", Revision: 999}
				case "invalid rank":
					v[0].Evidence.Ranking.Method = "invented"
				}
				return v, nil
			})
			client := checkClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
				checks++
				return mallFacts(req), nil
			})
			e, err := NewMallRetrieval(provider, client)
			require.NoError(t, err)
			got, err := e.Run(context.Background(), mallIntent())
			if mode == "vector" || mode == "forged category" || mode == "invalid rank" {
				require.NoError(t, err)
				if mode == "invalid rank" {
					require.EqualValues(t, 180, got.TotalPriceCents, "invalidly ranked a is excluded, not scored or revived")
				} else {
					require.EqualValues(t, 150, got.TotalPriceCents)
				}
				require.Equal(t, 2, checks)
				for _, item := range got.Items {
					require.Equal(t, "mall+rag", item.Source)
				}
			} else {
				require.Error(t, err)
				require.Nil(t, got)
			}
			legacy, err := NewMall(rankedSourceFunc(func(ctx context.Context, req tools.SearchProductsReq, _ int) ([]agent.ProductCandidate, error) {
				return provider.SearchProducts(ctx, req)
			}), client)
			require.NoError(t, err)
			_, err = legacy.Run(context.Background(), mallIntent())
			require.Error(t, err, "legacy keyword mode must not admit vector evidence")
		})
	}
	intent := mallIntent()
	intent.Keywords = nil
	req := mallRetrievalRequest(intent)
	require.NotEmpty(t, req.Query, "category-only plan still has a retrieval query")
	for range agent.MaxKeywords {
		intent.Keywords = append(intent.Keywords, strings.Repeat("长", agent.MaxKeywordRunes))
	}
	req = mallRetrievalRequest(intent)
	require.LessOrEqual(t, len(req.Keywords), agent.MaxKeywords)
	require.LessOrEqual(t, utf8.RuneCountInString(req.Query), agent.MaxQueryRunes)
}
