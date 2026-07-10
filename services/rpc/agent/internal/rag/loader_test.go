package rag

import (
	"context"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/cloudwego/eino/components/document"
	"google.golang.org/grpc"
)

// stubCatalog 是 mallCatalog 的测试替身，支持分页。
type stubCatalog struct {
	products []*pb.Product
	skus     map[string][]*pb.Sku
}

func (s *stubCatalog) ListProducts(ctx context.Context, in *pb.ListProductsReq, opts ...grpc.CallOption) (*pb.ListProductsResp, error) {
	return &pb.ListProductsResp{
		List:  paginate(s.products, in.Page, in.PageSize),
		Total: int64(len(s.products)),
	}, nil
}

func (s *stubCatalog) ListSkusByProduct(ctx context.Context, in *pb.ListSkusByProductReq, opts ...grpc.CallOption) (*pb.ListSkusByProductResp, error) {
	all := s.skus[in.ProductId]
	return &pb.ListSkusByProductResp{
		List:  paginate(all, in.Page, in.PageSize),
		Total: int64(len(all)),
	}, nil
}

func paginate[T any](all []T, page, pageSize int32) []T {
	start := int(page-1) * int(pageSize)
	end := start + int(pageSize)
	if start > len(all) {
		start = len(all)
	}
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}

// TestLoaderLoadsAllPages 验证跨页全量加载：3 个商品 pageSize=2 需两页，SKU 全部展开。
func TestLoaderLoadsAllPages(t *testing.T) {
	catalog := &stubCatalog{
		products: []*pb.Product{
			{Id: "p1", Name: "键盘", Providor: "K", Content: "适合办公"},
			{Id: "p2", Name: "台灯"},
			{Id: "p3", Name: "显示器"},
		},
		skus: map[string][]*pb.Sku{
			"p1": {{Id: "s1", Name: "红轴", Specs: `{"switch":"red"}`, Price: 29900, Stock: 10, Sold: 5}},
			"p2": {{Id: "s2", Price: 16900, Stock: 3}, {Id: "s3", Name: "Pro", Price: 26900, Stock: 2}},
			"p3": {{Id: "s4", Price: 69900, Stock: 1}},
		},
	}
	loader := NewMallProductLoader(catalog, 2)

	docs, err := loader.Load(context.Background(), document.Source{URI: SourceMallProducts})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(docs) != 4 {
		t.Fatalf("expected 4 sku documents, got %d", len(docs))
	}

	first := docs[0]
	if first.ID != "s1" {
		t.Fatalf("unexpected first doc: %+v", first)
	}
	if !strings.Contains(first.Content, "键盘 红轴") ||
		!strings.Contains(first.Content, "供应商: K") ||
		!strings.Contains(first.Content, "简介: 适合办公") {
		t.Fatalf("unexpected content: %q", first.Content)
	}
	// 价格/库存不进 embedding 文本，只进业务快照。
	if strings.Contains(first.Content, "29900") {
		t.Fatalf("price leaked into embedding content: %q", first.Content)
	}
	meta, ok := CandidateFromDocument(first)
	if !ok {
		t.Fatal("missing candidate metadata")
	}
	if meta.ProductID != "p1" || meta.PriceCents != 29900 || meta.Stock != 10 || meta.Source != "mall" {
		t.Fatalf("unexpected candidate metadata: %+v", meta)
	}
}
