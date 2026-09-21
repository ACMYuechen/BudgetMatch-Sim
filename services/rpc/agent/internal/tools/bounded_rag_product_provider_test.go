package tools

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBoundedVectorFirstFallbackAndEvidence(t *testing.T) {
	for _, mode := range []string{"vector", "empty", "unavailable", "invalid", "oversize", "payload", "nan", "latest unavailable", "parent conflict", "denied", "unauthenticated", "canceled", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			reads, fallbacks := 0, 0
			vector := vectorRetrieverFunc(func(ctx context.Context, _ string, k int) ([]*schema.Document, error) {
				reads++
				require.Equal(t, 2, k)
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				switch mode {
				case "empty":
					return nil, nil
				case "unavailable":
					return nil, errors.New("synthetic dependency failure")
				case "invalid":
					return []*schema.Document{nil}, nil
				case "oversize":
					return []*schema.Document{hybridDoc("a"), hybridDoc("b"), hybridDoc("c")}, nil
				case "payload":
					doc := hybridDoc("a")
					doc.Content = strings.Repeat("x", maxRankedResponseBytes+1)
					return []*schema.Document{doc}, nil
				case "nan":
					return []*schema.Document{hybridDoc("a").WithScore(math.NaN())}, nil
				case "latest unavailable":
					return []*schema.Document{candidateDoc("a", 100, 1), candidateDoc("a", 100, 0)}, nil
				case "parent conflict":
					return []*schema.Document{hybridDoc("a"), rag.NewCandidateDocument("a", "conflict", rag.CandidateMetadata{ProductId: "other", PriceCents: 100, Stock: 1})}, nil
				case "denied":
					return nil, status.Error(codes.PermissionDenied, "synthetic")
				case "unauthenticated":
					return nil, status.Error(codes.Unauthenticated, "synthetic")
				case "canceled":
					return nil, context.Canceled
				case "deadline":
					return nil, context.DeadlineExceeded
				}
				return []*schema.Document{hybridDoc("a")}, nil
			})
			keyword := rankedProviderFunc(func(_ context.Context, _ SearchProductsReq, k int) ([]ProductCandidate, error) {
				fallbacks++
				require.Equal(t, 2, k)
				return []ProductCandidate{hybridCandidate("fallback")}, nil
			})
			provider, err := NewBoundedRAGProductProvider(vector, keyword, rag.Config{TopK: 2})
			require.NoError(t, err)
			result, err := provider.SearchProducts(context.Background(), hybridRequest())
			require.Equal(t, 1, reads)
			switch mode {
			case "denied", "unauthenticated", "canceled", "deadline", "parent conflict":
				require.Error(t, err)
				require.Empty(t, result)
				require.Zero(t, fallbacks)
			case "vector":
				require.NoError(t, err)
				require.Zero(t, fallbacks)
				require.Equal(t, []string{"a"}, candidateIDs(result))
				require.Equal(t, agent.RetrievalMallVector, result[0].Evidence.Source)
				require.Empty(t, result[0].Evidence.Ranking.Method)
			default:
				require.NoError(t, err)
				require.Equal(t, 1, fallbacks)
				require.Equal(t, []string{"fallback"}, candidateIDs(result))
			}
		})
	}
}

func TestBoundedVectorRejectsBadFallbackAndStopsBeforeIO(t *testing.T) {
	for _, mode := range []string{"too many", "demo", "bad identity", "unbounded payload", "denied", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			provider, err := NewBoundedRAGProductProvider(&fakeRetriever{}, rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) {
				c := hybridCandidate("a")
				switch mode {
				case "too many":
					return []ProductCandidate{c, c, c}, nil
				case "demo":
					c.Evidence.State = agent.VerificationDemo
				case "bad identity":
					c.Evidence.ProductID = ""
				case "unbounded payload":
					c.Name = strings.Repeat("x", maxRankedResponseBytes+1)
				case "denied":
					return nil, status.Error(codes.PermissionDenied, "synthetic")
				case "cancel":
					cancel()
				}
				return []ProductCandidate{c}, nil
			}), rag.Config{TopK: 2})
			require.NoError(t, err)
			_, err = provider.SearchProducts(ctx, hybridRequest())
			require.Error(t, err)
		})
	}
	vector := vectorRetrieverFunc(func(context.Context, string, int) ([]*schema.Document, error) {
		t.Fatal("unexpected I/O")
		return nil, nil
	})
	keyword := rankedProviderFunc(func(context.Context, SearchProductsReq, int) ([]ProductCandidate, error) {
		t.Fatal("unexpected I/O")
		return nil, nil
	})
	for _, topK := range []int{-1, 33} {
		_, err := NewBoundedRAGProductProvider(vector, keyword, rag.Config{TopK: topK})
		require.Error(t, err)
	}
	_, err := NewBoundedRAGProductProvider(nil, keyword, rag.Config{})
	require.Error(t, err)
	_, err = NewBoundedRAGProductProvider(vector, nil, rag.Config{})
	require.Error(t, err)
	_, err = NewBoundedRAGProductProvider(vector, keyword, hybridConfig(2))
	require.Error(t, err)
	p, err := NewBoundedRAGProductProvider(vector, keyword, rag.Config{})
	require.NoError(t, err)
	_, err = p.SearchProducts(context.Background(), SearchProductsReq{})
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.SearchProducts(ctx, hybridRequest())
	require.ErrorIs(t, err, context.Canceled)
}
