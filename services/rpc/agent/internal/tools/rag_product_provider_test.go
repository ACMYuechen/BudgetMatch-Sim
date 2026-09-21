package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeRetriever 返回预置文档或错误，并记录收到的 TopK。
type fakeRetriever struct {
	docs     []*schema.Document
	err      error
	gotTopK  int
	gotQuery string
}

func TestRAGCandidateCarriesInternalUnverifiedEvidence(t *testing.T) {
	doc := candidateDoc("s1", 100, 2).WithScore(.9)
	meta, _ := rag.CandidateFromDocument(doc)
	meta.SnapshotAtUnixMs = 1234
	doc = rag.NewCandidateDocument(doc.ID, doc.Content, meta).WithScore(.9)
	provider := NewRAGProductProvider(&fakeRetriever{docs: []*schema.Document{doc}}, &fakeFallbackProvider{}, 10)
	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{Query: "keyboard"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	e := got[0].Evidence
	require.Equal(t, agentcore.RetrievalMallVector, e.Source)
	require.Equal(t, "p-s1", e.ProductID)
	require.EqualValues(t, 1234, e.SnapshotAtUnixMs)
	require.Equal(t, .9, e.Relevance)
	require.True(t, e.HasRelevance)
	require.Positive(t, e.RetrievedAtUnixMs)
	require.Equal(t, agentcore.VerificationUnverified, e.State)
	require.Zero(t, e.VerifiedAtUnixMs)
	data, err := json.Marshal(got[0])
	require.NoError(t, err)
	require.NotContains(t, string(data), "Evidence")
	require.NotContains(t, string(data), "Relevance")
	for _, score := range []float64{math.NaN(), math.Inf(1)} {
		fallback := &fakeFallbackProvider{}
		_, err := NewRAGProductProvider(&fakeRetriever{docs: []*schema.Document{doc.WithScore(score)}}, fallback, 10).
			SearchProducts(context.Background(), SearchProductsReq{Query: "keyboard"})
		require.NoError(t, err)
		require.True(t, fallback.called)
	}
}

func TestRAGTerminalErrorsNeverUseKeywordFallback(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded,
		status.Error(codes.Unauthenticated, "denied"), status.Error(codes.PermissionDenied, "denied")} {
		fallback := &fakeFallbackProvider{}
		_, err := NewRAGProductProvider(&fakeRetriever{err: failure}, fallback, 10).
			SearchProducts(context.Background(), SearchProductsReq{Query: "keyboard"})
		require.ErrorIs(t, err, failure)
		require.False(t, fallback.called)
	}
}

func (f *fakeRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	f.gotQuery = query
	co := retriever.GetCommonOptions(&retriever.Options{}, opts...)
	if co.TopK != nil {
		f.gotTopK = *co.TopK
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.docs, nil
}

// fakeFallbackProvider 记录是否被调用。
type fakeFallbackProvider struct {
	called  bool
	calls   int
	err     error
	request SearchProductsReq
	result  []ProductCandidate
}

func (f *fakeFallbackProvider) Name() string { return "fake.fallback" }

func (f *fakeFallbackProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	f.called = true
	f.calls++
	f.request = req
	return f.result, f.err
}

func candidateDoc(id string, price, stock int64) *schema.Document {
	return rag.NewCandidateDocument(id, "内容", rag.CandidateMetadata{
		ProductId:  "p-" + id,
		Name:       "商品" + id,
		Category:   "cat",
		PriceCents: price,
		Stock:      stock,
		Sold:       10,
		Source:     "mall",
	})
}

// TestRAGProviderConvertsAndFilters 验证检索结果还原候选并按预算/库存过滤。
func TestRAGProviderConvertsAndFilters(t *testing.T) {
	retr := &fakeRetriever{docs: []*schema.Document{
		candidateDoc("s1", 20000, 5),
		candidateDoc("s2", 90000, 5), // 超预算
		candidateDoc("s3", 10000, 0), // 无库存
	}}
	fallback := &fakeFallbackProvider{}
	provider := NewRAGProductProvider(retr, fallback, 20)

	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{
		Query:       "安静的办公键盘",
		Keywords:    []string{"键盘"},
		BudgetCents: 50000,
		MaxItems:    3,
	})
	if err != nil {
		t.Fatalf("SearchProducts() error = %v", err)
	}
	if fallback.called {
		t.Fatal("fallback should not be called when rag returns usable candidates")
	}
	if len(got) != 1 || got[0].Id != "s1" || got[0].Source != "mall+rag" {
		t.Fatalf("unexpected candidates: %+v", got)
	}
	if retr.gotQuery != "安静的办公键盘 键盘" {
		t.Fatalf("unexpected semantic query: %q", retr.gotQuery)
	}
	if retr.gotTopK != 12 { // MaxItems*4 冗余召回
		t.Fatalf("expected topK 12, got %d", retr.gotTopK)
	}
}

// TestRAGProviderCapsTopK 验证召回量被配置上限夹住。
func TestRAGProviderCapsTopK(t *testing.T) {
	retr := &fakeRetriever{docs: []*schema.Document{candidateDoc("s1", 100, 1)}}
	provider := NewRAGProductProvider(retr, &fakeFallbackProvider{}, 10)

	if _, err := provider.SearchProducts(context.Background(), SearchProductsReq{Query: "键盘", MaxItems: 5}); err != nil {
		t.Fatalf("SearchProducts() error = %v", err)
	}
	if retr.gotTopK != 10 { // 5*4=20 被上限 10 夹住
		t.Fatalf("expected capped topK 10, got %d", retr.gotTopK)
	}
}

// TestRAGProviderFallsBackOnError 验证检索出错时回退关键词 provider 且不上抛错误。
func TestRAGProviderFallsBackOnError(t *testing.T) {
	retr := &fakeRetriever{err: errors.New("pg down")}
	fallback := &fakeFallbackProvider{result: []ProductCandidate{{Id: "kw-1", Stock: 1}}}
	provider := NewRAGProductProvider(retr, fallback, 10)

	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{Query: "键盘"})
	if err != nil {
		t.Fatalf("expected graceful fallback, got error %v", err)
	}
	if !fallback.called || len(got) != 1 || got[0].Id != "kw-1" {
		t.Fatalf("expected fallback result, got %+v", got)
	}
}

// TestRAGProviderFallsBackOnEmpty 验证检索为空（如首轮同步未完成）时回退。
func TestRAGProviderFallsBackOnEmpty(t *testing.T) {
	retr := &fakeRetriever{}
	fallback := &fakeFallbackProvider{result: []ProductCandidate{{Id: "kw-1", Stock: 1}}}
	provider := NewRAGProductProvider(retr, fallback, 10)

	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{Query: "键盘"})
	if err != nil {
		t.Fatalf("SearchProducts() error = %v", err)
	}
	if !fallback.called || len(got) != 1 {
		t.Fatalf("expected fallback on empty retrieval, got %+v", got)
	}
}

func TestRAGProviderFiltersBeforeDecidingFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		docs []*schema.Document
	}{
		{"zero price", []*schema.Document{candidateDoc("s1", 0, 1)}},
		{"negative price", []*schema.Document{candidateDoc("s1", -1, 1)}},
		{"price beyond domain", []*schema.Document{candidateDoc("s1", agentcore.MaxBudgetCents+1, 1)}},
		{"empty identity", []*schema.Document{candidateDoc("", 100, 1)}},
		{"padded identity", []*schema.Document{candidateDoc(" s1", 100, 1)}},
		{"latest zero price", []*schema.Document{candidateDoc("s1", 100, 1), candidateDoc("s1", 0, 1)}},
		{"latest unavailable", []*schema.Document{candidateDoc("s1", 100, 1), candidateDoc("s1", 100, 0)}},
		{"latest over budget", []*schema.Document{candidateDoc("s1", 100, 1), candidateDoc("s1", 600, 1)}},
		{"latest invalid score", []*schema.Document{candidateDoc("s1", 100, 1), candidateDoc("s1", 100, 1).WithScore(math.NaN())}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := []ProductCandidate{{Id: "keyword", PriceCents: 200, Stock: 1}}
			fallback := &fakeFallbackProvider{result: want}
			got, err := NewRAGProductProvider(&fakeRetriever{docs: tc.docs}, fallback, 8).
				SearchProducts(context.Background(), SearchProductsReq{Query: "keyboard", BudgetCents: 500, MaxItems: 1})
			require.NoError(t, err)
			require.True(t, fallback.called)
			require.Equal(t, 1, fallback.calls)
			require.Equal(t, want, got)
		})
	}
}

func TestRAGProviderPreservesOrderAndLatestSnapshotWithoutUnneededFallback(t *testing.T) {
	first := candidateDoc("s1", 100, 1)
	latest := candidateDoc("s1", 200, 2).WithScore(.8)
	docs := []*schema.Document{first, candidateDoc("invalid", 0, 1), candidateDoc("s2", 300, 1), latest}
	fallback := &fakeFallbackProvider{}
	got, err := NewRAGProductProvider(&fakeRetriever{docs: docs}, fallback, 8).
		SearchProducts(context.Background(), SearchProductsReq{Query: "keyboard", BudgetCents: 500, MaxItems: 2})
	require.NoError(t, err)
	require.False(t, fallback.called)
	require.Len(t, got, 2)
	require.Equal(t, "s1", got[0].Id)
	require.EqualValues(t, 200, got[0].PriceCents)
	require.EqualValues(t, 2, got[0].Stock)
	require.Equal(t, .8, got[0].Evidence.Relevance)
	require.Equal(t, "s2", got[1].Id)
	meta, ok := rag.CandidateFromDocument(first)
	require.True(t, ok)
	require.EqualValues(t, 100, meta.PriceCents) // The source documents were not mutated.
}

func TestRAGProviderInvalidSnapshotsPreserveFallbackRequestAndError(t *testing.T) {
	for _, failure := range []error{status.Error(codes.Unavailable, "unavailable"), status.Error(codes.PermissionDenied, "denied"), context.Canceled} {
		fallback := &fakeFallbackProvider{err: failure}
		retr := &fakeRetriever{docs: []*schema.Document{candidateDoc("s1", 0, 1)}}
		req := SearchProductsReq{Query: "keyboard", Keywords: []string{"quiet"}, BudgetCents: 500, MaxItems: 2}
		got, err := NewRAGProductProvider(retr, fallback, 8).SearchProducts(context.Background(), req)
		require.ErrorIs(t, err, failure)
		require.Empty(t, got)
		require.Equal(t, 1, fallback.calls)
		require.Equal(t, req, fallback.request)
		require.Equal(t, 8, retr.gotTopK)
	}
}
