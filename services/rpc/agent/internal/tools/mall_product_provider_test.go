package tools

import (
	"context"
	"testing"

	"budgetmatch-sim/services/rpc/mall/pb"

	"google.golang.org/grpc"
)

// stubProductService 是 productSearcher 的测试替身，按关键词返回预置商品分页。
type stubProductService struct {
	productsByKeyword map[string][]*pb.Product
	skusByProduct     map[string][]*pb.Sku
	listCalls         []string // 记录 ListProducts 收到的 keyword+page，用于断言分页行为
}

func (s *stubProductService) ListProducts(ctx context.Context, in *pb.ListProductsReq, opts ...grpc.CallOption) (*pb.ListProductsResp, error) {
	s.listCalls = append(s.listCalls, in.Keyword)
	all := s.productsByKeyword[in.Keyword]

	start := int(in.Page-1) * int(in.PageSize)
	end := start + int(in.PageSize)
	if start > len(all) {
		start = len(all)
	}
	if end > len(all) {
		end = len(all)
	}
	return &pb.ListProductsResp{
		List:     all[start:end],
		Total:    int64(len(all)),
		Page:     in.Page,
		PageSize: in.PageSize,
	}, nil
}

func (s *stubProductService) ListSkusByProduct(ctx context.Context, in *pb.ListSkusByProductReq, opts ...grpc.CallOption) (*pb.ListSkusByProductResp, error) {
	skus := s.skusByProduct[in.ProductId]
	return &pb.ListSkusByProductResp{
		List:  skus,
		Total: int64(len(skus)),
		Page:  in.Page,
	}, nil
}

// TestMallProviderMapsSkuToCandidate 验证商品+SKU 到候选的字段映射与库存/预算过滤。
func TestMallProviderMapsSkuToCandidate(t *testing.T) {
	stub := &stubProductService{
		productsByKeyword: map[string][]*pb.Product{
			"键盘": {{Id: "p1", Name: "静音机械键盘", Providor: "Keychron"}},
		},
		skusByProduct: map[string][]*pb.Sku{
			"p1": {
				{Id: "s1", ProductId: "p1", Name: "红轴", Specs: `{"switch":"red"}`, Price: 29900, Stock: 10, Sold: 200},
				{Id: "s2", ProductId: "p1", Name: "青轴", Price: 39900, Stock: 0, Sold: 50},     // 无库存,应被过滤
				{Id: "s3", ProductId: "p1", Name: "旗舰版", Price: 99900, Stock: 5, Sold: 10},   // 超预算,应被过滤
			},
		},
	}
	provider := NewMallProductProvider(stub)

	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{
		Query:       "买个键盘",
		Keywords:    []string{"键盘"},
		BudgetCents: 50000,
	})
	if err != nil {
		t.Fatalf("SearchProducts() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate after stock/budget filter, got %d: %+v", len(got), got)
	}

	c := got[0]
	if c.ID != "s1" || c.Name != "静音机械键盘 红轴" || c.Category != "" || c.Source != "mall" {
		t.Fatalf("unexpected candidate mapping: %+v", c)
	}
	if c.PriceCents != 29900 || c.Stock != 10 || c.Sold != 200 {
		t.Fatalf("sku numeric fields not mapped: %+v", c)
	}
	if len(c.Tags) != 2 || c.Tags[0] != "Keychron" {
		t.Fatalf("expected brand+specs tags, got %+v", c.Tags)
	}
}

// TestMallProviderDedupsAcrossTerms 验证多个搜索词命中同一商品时只保留一份。
func TestMallProviderDedupsAcrossTerms(t *testing.T) {
	shared := &pb.Product{Id: "p1", Name: "台灯"}
	stub := &stubProductService{
		productsByKeyword: map[string][]*pb.Product{
			"台灯": {shared},
			"护眼": {shared},
		},
		skusByProduct: map[string][]*pb.Sku{
			"p1": {{Id: "s1", Price: 16900, Stock: 3}},
		},
	}
	provider := NewMallProductProvider(stub)

	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{
		Keywords: []string{"台灯", "护眼"},
	})
	if err != nil {
		t.Fatalf("SearchProducts() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected deduped single candidate, got %d", len(got))
	}
}

// TestMallProviderCapsPagination 验证每词翻页封顶：40 个商品 20/页只翻 mallMaxPagesPerTerm 页,
// 且候选总量不超过 mallMaxProducts。
func TestMallProviderCapsPagination(t *testing.T) {
	var many []*pb.Product
	skus := make(map[string][]*pb.Sku)
	for i := range 80 {
		id := "p" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		many = append(many, &pb.Product{Id: id, Name: "商品" + id})
		skus[id] = []*pb.Sku{{Id: "s-" + id, Price: 100, Stock: 1}}
	}
	stub := &stubProductService{
		productsByKeyword: map[string][]*pb.Product{"多": many},
		skusByProduct:     skus,
	}
	provider := NewMallProductProvider(stub)

	got, err := provider.SearchProducts(context.Background(), SearchProductsReq{Keywords: []string{"多"}})
	if err != nil {
		t.Fatalf("SearchProducts() error = %v", err)
	}
	if len(got) > mallMaxProducts {
		t.Fatalf("candidates exceed cap: %d > %d", len(got), mallMaxProducts)
	}
	if calls := len(stub.listCalls); calls > mallMaxPagesPerTerm {
		t.Fatalf("pagination not capped: %d calls", calls)
	}
}

// TestMallProviderSearchTerms 验证搜索词归并：关键词优先、查询兜底、大小写去重。
func TestMallProviderSearchTerms(t *testing.T) {
	terms := searchTerms(SearchProductsReq{
		Query:    "Study desk",
		Keywords: []string{"study", "  ", "desk", "STUDY"},
	})
	if len(terms) != 3 || terms[0] != "study" || terms[1] != "desk" || terms[2] != "Study desk" {
		t.Fatalf("unexpected terms: %+v", terms)
	}
}
