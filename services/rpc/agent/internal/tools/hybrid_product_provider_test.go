package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rankedProviderFunc func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error)

func (f rankedProviderFunc) SearchRankedProducts(ctx context.Context, req SearchProductsReq, k int) ([]ProductCandidate, error) {
	return f(ctx, req, k)
}

type vectorRetrieverFunc func(context.Context, string, int) ([]*schema.Document, error)

func (f vectorRetrieverFunc) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	k := retriever.GetCommonOptions(&retriever.Options{}, opts...).TopK
	return f(ctx, query, *k)
}
func hybridCandidate(id string) ProductCandidate {
	return ProductCandidate{Id: id, Name: id, Source: "mall", PriceCents: 100, Stock: 2,
		Evidence: agentcore.CandidateEvidence{Source: agentcore.RetrievalMallKeyword, ProductID: "p-" + id}}
}
func hybridDoc(id string) *schema.Document {
	c := hybridCandidate(id)
	return rag.NewCandidateDocument(id, "synthetic text", rag.CandidateMetadata{ProductId: c.Evidence.ProductID,
		Name: c.Name, PriceCents: c.PriceCents, Stock: c.Stock, Source: "mall"}).WithScore(.7)
}
func hybridConfig(k int) rag.Config {
	return rag.Config{TopK: k, Retrieval: rag.RetrievalConfig{Strategy: rag.StrategyHybridRRF, InitialK: 4, MaxK: 8}}
}
func hybridRequest() SearchProductsReq {
	return SearchProductsReq{Query: "private keyboard request", Keywords: []string{"keyboard"}, BudgetCents: 1000, MaxItems: 2}
}
func candidateIDs(candidates []ProductCandidate) []string {
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.Id
	}
	return ids
}

func TestHybridRankFusionUsesRanksAndKeepsEvidence(t *testing.T) {
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) {
		return []*schema.Document{hybridDoc("b").WithScore(.01), hybridDoc("c").WithScore(.99)}, nil
	}), rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) {
		return []ProductCandidate{hybridCandidate("a"), hybridCandidate("b")}, nil
	}), hybridConfig(3))
	require.NoError(t, err)
	result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
	require.NoError(t, err)
	require.Equal(t, []string{"b", "a", "c"}, candidateIDs(result.Candidates))
	e := result.Candidates[0].Evidence
	require.Equal(t, agentcore.RetrievalMallKeyword, e.Source)
	require.Equal(t, 2, e.Ranking.KeywordRank)
	require.Equal(t, 1, e.Ranking.VectorRank)
	require.InDelta(t, 1.0/62+1.0/61, e.Ranking.FusionScore, 1e-12)
	require.Equal(t, .01, e.Relevance, "raw similarity must not become the RRF score")
	require.Equal(t, agentcore.VerificationUnverified, e.State)
	require.Len(t, result.Calls, 3)
	require.Equal(t, "retrieval.keyword", result.Calls[0].Name)
	require.Equal(t, "retrieval.vector", result.Calls[1].Name)
	data, err := json.Marshal(result.Candidates)
	require.NoError(t, err)
	require.NotContains(t, string(data), "Ranking")
	require.Equal(t, result.Calls, safety.ToolCalls(result.Calls))
}

func TestRankFusionDeduplicatesBeforeFilteringAndRejectsIdentityConflict(t *testing.T) {
	kw := []ProductCandidate{hybridCandidate("a"), hybridCandidate("b"), hybridCandidate("a"), hybridCandidate("c")}
	kw[2].Stock = 0
	kw[1].PriceCents = 1500
	vector := []ProductCandidate{hybridCandidate("a"), hybridCandidate("b"), hybridCandidate("c"), hybridCandidate("d")}
	vector[2].Evidence.ProductID = "different-parent"
	got, conflicts := fuseRankings(kw, vector, 1000, 10)
	require.Equal(t, []string{"d"}, candidateIDs(got))
	require.Equal(t, 1, conflicts)
	require.Equal(t, 4, got[0].Evidence.Ranking.VectorRank, "do not compact ranks after filtering")
	got, _ = fuseRankings([]ProductCandidate{hybridCandidate("b"), hybridCandidate("b")}, []ProductCandidate{hybridCandidate("a")}, 1000, 10)
	require.Equal(t, []string{"a", "b"}, candidateIDs(got), "ties use ID byte order; duplicate lane votes do not stack")
	require.InDelta(t, 1.0/61, got[1].Evidence.Ranking.FusionScore, 1e-12)
}

func TestRankFusionRejectsParentChangesWithinEitherLane(t *testing.T) {
	changed := hybridCandidate("a")
	changed.Evidence.ProductID = "other-parent"
	conflicting := []ProductCandidate{hybridCandidate("a"), changed, hybridCandidate("a")}
	for _, lanes := range [][2][]ProductCandidate{
		{conflicting, nil}, {nil, conflicting}, {conflicting, {hybridCandidate("a")}},
	} {
		got, conflicts := fuseRankings(lanes[0], lanes[1], 1000, 4)
		require.Empty(t, got, "later copies must not clear an earlier identity conflict")
		require.Equal(t, 1, conflicts)
	}
}

func TestHybridRetainsFirstAttemptConflictDiagnosticAfterExpansion(t *testing.T) {
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) {
		return []*schema.Document{hybridDoc("a")}, nil
	}), rankedProviderFunc(func(_ context.Context, _ SearchProductsReq, k int) ([]ProductCandidate, error) {
		if k == 4 {
			a := hybridCandidate("a")
			a.Evidence.ProductID = "conflicting-parent"
			return []ProductCandidate{a}, nil
		}
		return []ProductCandidate{hybridCandidate("b"), hybridCandidate("c")}, nil
	}), hybridConfig(2))
	require.NoError(t, err)
	result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
	require.NoError(t, err)
	var names []string
	for _, call := range result.Calls {
		names = append(names, call.Name)
	}
	require.Equal(t, []string{"retrieval.keyword", "retrieval.vector", "retrieval.conflict", "retrieval.expand", "retrieval.keyword", "retrieval.vector", "retrieval.fusion"}, names)
}

func TestHybridExpandsOnceAndDiscardsSupersededFacts(t *testing.T) {
	var kwWindows, vectorWindows []int
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(_ context.Context, _ string, k int) ([]*schema.Document, error) {
		vectorWindows = append(vectorWindows, k)
		return []*schema.Document{hybridDoc("a")}, nil
	}), rankedProviderFunc(func(_ context.Context, _ SearchProductsReq, k int) ([]ProductCandidate, error) {
		kwWindows = append(kwWindows, k)
		if k == 4 {
			return []ProductCandidate{hybridCandidate("a")}, nil
		}
		a := hybridCandidate("a")
		a.Stock = 0
		return []ProductCandidate{a, hybridCandidate("b"), hybridCandidate("c")}, nil
	}), hybridConfig(2))
	require.NoError(t, err)
	result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
	require.NoError(t, err)
	require.Equal(t, []int{4, 8}, kwWindows)
	require.Equal(t, []int{4, 8}, vectorWindows)
	require.Equal(t, []string{"b", "c"}, candidateIDs(result.Candidates))
	require.Len(t, result.Calls, 6)
	require.Equal(t, "retrieval.expand", result.Calls[2].Name)
}

func TestHybridDegradesWithoutRetryingFailedLaneOrLeakingErrors(t *testing.T) {
	var vectorCalls atomic.Int32
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) {
		vectorCalls.Add(1)
		return nil, status.Error(codes.Unavailable, "SECRET_UPSTREAM_PRIVATE_QUERY")
	}), rankedProviderFunc(func(_ context.Context, _ SearchProductsReq, k int) ([]ProductCandidate, error) {
		if k == 4 {
			return []ProductCandidate{hybridCandidate("a")}, nil
		}
		return []ProductCandidate{hybridCandidate("a"), hybridCandidate("b")}, nil
	}), hybridConfig(2))
	require.NoError(t, err)
	result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
	require.NoError(t, err)
	require.EqualValues(t, 1, vectorCalls.Load())
	require.Equal(t, []string{"a", "b"}, candidateIDs(result.Candidates))
	require.False(t, result.Calls[1].Success)
	require.Contains(t, result.Calls[1].Detail, "rpc_unavailable")
	data, err := json.Marshal(safety.ToolCalls(result.Calls))
	require.NoError(t, err)
	require.NotContains(t, string(data), "SECRET")
	require.NotContains(t, string(data), "private keyboard")
}

func TestHybridNeverHidesFailureAsSuccessfulEmptyResult(t *testing.T) {
	for _, failKeyword := range []bool{false, true} {
		provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) {
			return nil, errors.New("vector failure")
		}),
			rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) {
				if failKeyword {
					return nil, errors.New("keyword failure")
				}
				return nil, nil
			}), hybridConfig(2))
		require.NoError(t, err)
		result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
		require.Equal(t, codes.Unavailable, status.Code(err))
		require.Empty(t, result.Candidates)
	}
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) { return nil, nil }),
		rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) { return nil, nil }), hybridConfig(2))
	require.NoError(t, err)
	result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
	require.NoError(t, err)
	require.Empty(t, result.Candidates)
}

func TestHybridFailedExpansionDoesNotReviveEarlierLane(t *testing.T) {
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) { return nil, nil }),
		rankedProviderFunc(func(_ context.Context, _ SearchProductsReq, k int) ([]ProductCandidate, error) {
			if k == 4 {
				return []ProductCandidate{hybridCandidate("a")}, nil
			}
			return nil, errors.New("expansion failed")
		}), hybridConfig(2))
	require.NoError(t, err)
	result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Empty(t, result.Candidates)
}

func TestHybridAuthFailureCancelsSiblingAndVetoesFusion(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated} {
		started, exited := make(chan struct{}), make(chan struct{})
		provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) {
			<-started
			return nil, status.Error(code, "SECRET_DENIAL")
		}), rankedProviderFunc(func(ctx context.Context, _ SearchProductsReq, _ int) ([]ProductCandidate, error) {
			close(started)
			<-ctx.Done()
			close(exited)
			return nil, ctx.Err()
		}), hybridConfig(2))
		require.NoError(t, err)
		result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
		require.Equal(t, code, status.Code(err))
		require.Empty(t, result.Candidates)
		require.NotContains(t, err.Error(), "SECRET")
		select {
		case <-exited:
		default:
			t.Fatal("worker still running after return")
		}
	}
}

func TestHybridPrivateLaneTimeoutCanDegradeButParentDeadlineStops(t *testing.T) {
	cfg := hybridConfig(1)
	cfg.Retrieval.TimeoutMillis = 100
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(ctx context.Context, _ string, _ int) ([]*schema.Document, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}),
		rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) {
			return []ProductCandidate{hybridCandidate("a")}, nil
		}), cfg)
	require.NoError(t, err)
	result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
	require.NoError(t, err)
	require.Len(t, result.Candidates, 1)
	require.False(t, result.Calls[1].Success)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	result, err = provider.SearchProductsWithTrace(ctx, hybridRequest())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, result.Candidates)
}

func TestHybridRejectsMalformedOrOversizedLane(t *testing.T) {
	for _, makeDocs := range []func() []*schema.Document{
		func() []*schema.Document { return []*schema.Document{nil} },
		func() []*schema.Document { return []*schema.Document{{ID: "missing-meta"}} },
		func() []*schema.Document { return []*schema.Document{hybridDoc("a").WithScore(math.NaN())} },
		func() []*schema.Document {
			d := hybridDoc("a")
			d.Content = strings.Repeat("x", maxRankedResponseBytes)
			return []*schema.Document{d}
		},
		func() []*schema.Document {
			return []*schema.Document{hybridDoc("a"), hybridDoc("b"), hybridDoc("c"), hybridDoc("d"), hybridDoc("e")}
		},
	} {
		provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) { return makeDocs(), nil }),
			rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) {
				return []ProductCandidate{hybridCandidate("safe")}, nil
			}), hybridConfig(1))
		require.NoError(t, err)
		result, err := provider.SearchProductsWithTrace(context.Background(), hybridRequest())
		require.NoError(t, err)
		require.Equal(t, []string{"safe"}, candidateIDs(result.Candidates))
		require.False(t, result.Calls[1].Success)
	}
	bad := hybridCandidate("mock")
	bad.Evidence.Source = agentcore.RetrievalDemo
	require.Error(t, validateRanking([]ProductCandidate{bad}, 0, 4))
}

func TestHybridConcurrentRequestsDoNotShareTraceOrQueryState(t *testing.T) {
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(_ context.Context, q string, _ int) ([]*schema.Document, error) {
		return []*schema.Document{hybridDoc(strings.Fields(q)[0])}, nil
	}), rankedProviderFunc(func(_ context.Context, req SearchProductsReq, _ int) ([]ProductCandidate, error) {
		req.Keywords[0] = "mutated privately"
		return []ProductCandidate{hybridCandidate(req.Query)}, nil
	}), hybridConfig(1))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := hybridRequest()
			req.Query = fmt.Sprintf("sku-%02d", i)
			result, err := provider.SearchProductsWithTrace(context.Background(), req)
			if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Id != req.Query || len(result.Calls) != 3 || req.Keywords[0] != "keyboard" {
				t.Errorf("cross-request state: result=%+v err=%v", result, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestHybridInvalidAndCanceledRequestsNeverReachSources(t *testing.T) {
	var calls atomic.Int32
	provider, err := NewHybridProductProvider(vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) { calls.Add(1); return nil, nil }),
		rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) {
			calls.Add(1)
			return nil, nil
		}), hybridConfig(1))
	require.NoError(t, err)
	for _, req := range []SearchProductsReq{{}, {Query: "q", BudgetCents: -1, MaxItems: 1}, {Query: strings.Repeat("q", agentcore.MaxQueryRunes+1), BudgetCents: 100, MaxItems: 1}} {
		_, err := provider.SearchProducts(context.Background(), req)
		require.ErrorIs(t, err, agentcore.ErrInvalidInput)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.SearchProducts(ctx, hybridRequest())
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls.Load())
}
