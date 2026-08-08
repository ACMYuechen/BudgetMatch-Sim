package tools

import (
	"context"
	"errors"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/rag"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

// fakeRetriever 返回预置文档或错误，并记录收到的 TopK。
type fakeRetriever struct {
	docs     []*schema.Document
	err      error
	gotTopK  int
	gotQuery string
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
	called bool
	result []ProductCandidate
}

func (f *fakeFallbackProvider) Name() string { return "fake.fallback" }

func (f *fakeFallbackProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	f.called = true
	return f.result, nil
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
	provider := NewRAGProductProvider(retr, fallback, nil, 20)

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
	provider := NewRAGProductProvider(retr, &fakeFallbackProvider{}, nil, 10)

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
	provider := NewRAGProductProvider(retr, fallback, nil, 10)

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
	provider := NewRAGProductProvider(retr, fallback, nil, 10)

	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{Query: "键盘"})
	if err != nil {
		t.Fatalf("SearchProducts() error = %v", err)
	}
	if !fallback.called || len(got) != 1 {
		t.Fatalf("expected fallback on empty retrieval, got %+v", got)
	}
}
