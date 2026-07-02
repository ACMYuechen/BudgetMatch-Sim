package rag

import (
	"context"
	"fmt"
	"strings"

	"budgetmatch-sim/services/rpc/mall/client/productservice"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/grpc"
)

const (
	// SourceMallProducts 是商品全量加载的约定 Source URI。
	SourceMallProducts = "mall://products"

	// loaderStatusOnShelf 上架状态。
	loaderStatusOnShelf = 1
	// detailLimit 限制进入 embedding 文本的商品简介长度。
	detailLimit = 500
)

// mallCatalog 是 Loader 依赖的 mall 商品能力子集。
type mallCatalog interface {
	ListProducts(ctx context.Context, in *productservice.ListProductsReq, opts ...grpc.CallOption) (*productservice.ListProductsResp, error)
	ListSkusByProduct(ctx context.Context, in *productservice.ListSkusByProductReq, opts ...grpc.CallOption) (*productservice.ListSkusByProductResp, error)
}

// MallProductLoader 实现 eino document.Loader：分页拉取 mall 全量上架商品与 SKU，
// 每个上架 SKU 产出一个 Document（Content 为语义文本，业务快照进 MetaData）。
// 与在线检索链路不同，离线同步需要全量数据，这里不设翻页上限。
type MallProductLoader struct {
	client   mallCatalog
	pageSize int32
}

// 确保 MallProductLoader 实现 document.Loader。
var _ document.Loader = (*MallProductLoader)(nil)

// NewMallProductLoader 创建 mall 商品加载器。
func NewMallProductLoader(client mallCatalog, pageSize int32) *MallProductLoader {
	if pageSize <= 0 {
		pageSize = defaultSyncPageSize
	}
	return &MallProductLoader{client: client, pageSize: pageSize}
}

// Load 全量加载上架商品文档。
func (l *MallProductLoader) Load(ctx context.Context, src document.Source, opts ...document.LoaderOption) (docs []*schema.Document, err error) {
	ctx = callbacks.EnsureRunInfo(ctx, l.GetType(), components.ComponentOfLoader)
	ctx = callbacks.OnStart(ctx, &document.LoaderCallbackInput{Source: src})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	for page := int32(1); ; page++ {
		resp, listErr := l.client.ListProducts(ctx, &productservice.ListProductsReq{
			Page:     page,
			PageSize: l.pageSize,
			Status:   loaderStatusOnShelf,
		})
		if listErr != nil {
			return nil, fmt.Errorf("rag: list products page %d: %w", page, listErr)
		}
		for _, product := range resp.GetList() {
			if product == nil {
				continue
			}
			productDocs, skuErr := l.loadSkuDocs(ctx, product)
			if skuErr != nil {
				return nil, skuErr
			}
			docs = append(docs, productDocs...)
		}
		if int64(page)*int64(l.pageSize) >= resp.GetTotal() || len(resp.GetList()) == 0 {
			break
		}
	}

	callbacks.OnEnd(ctx, &document.LoaderCallbackOutput{Source: src, Docs: docs})
	return docs, nil
}

// loadSkuDocs 拉取商品的全部上架 SKU 并生成文档。
func (l *MallProductLoader) loadSkuDocs(ctx context.Context, product *productservice.Product) ([]*schema.Document, error) {
	var docs []*schema.Document
	for page := int32(1); ; page++ {
		resp, err := l.client.ListSkusByProduct(ctx, &productservice.ListSkusByProductReq{
			ProductId: product.Id,
			Page:      page,
			PageSize:  l.pageSize,
			Status:    loaderStatusOnShelf,
		})
		if err != nil {
			return nil, fmt.Errorf("rag: list skus of product %s: %w", product.Id, err)
		}
		for _, sku := range resp.GetList() {
			if sku == nil {
				continue
			}
			docs = append(docs, buildSkuDocument(product, sku))
		}
		if int64(page)*int64(l.pageSize) >= resp.GetTotal() || len(resp.GetList()) == 0 {
			break
		}
	}
	return docs, nil
}

// buildSkuDocument 组装单个 SKU 的文档：语义文本进 Content，业务快照进 MetaData。
func buildSkuDocument(product *productservice.Product, sku *productservice.Sku) *schema.Document {
	name := joinNonEmpty(" ", product.Name, sku.Name)
	content := buildContent(product, sku, name)
	meta := CandidateMetadata{
		ProductID:  product.Id,
		Name:       name,
		Category:   product.CategoryId,
		Brand:      product.Brand,
		PriceCents: sku.Price,
		Stock:      sku.Stock,
		Sold:       sku.Sold,
		Source:     "mall",
		Tags:       loaderTags(product.Brand, sku.Specs),
	}
	return NewCandidateDocument(sku.Id, content, meta)
}

// buildContent 组装参与 embedding 的语义文本。刻意不含价格/库存/销量：
// 这些高频波动字段进 MetaData，避免每次价格变动都触发重嵌入。
func buildContent(product *productservice.Product, sku *productservice.Sku, name string) string {
	var b strings.Builder
	b.WriteString("商品: ")
	b.WriteString(name)
	if brand := strings.TrimSpace(product.Brand); brand != "" {
		b.WriteString("\n品牌: ")
		b.WriteString(brand)
	}
	if category := strings.TrimSpace(product.CategoryId); category != "" {
		b.WriteString("\n分类: ")
		b.WriteString(category)
	}
	if specs := strings.TrimSpace(sku.Specs); specs != "" {
		b.WriteString("\n规格: ")
		b.WriteString(specs)
	}
	if detail := strings.TrimSpace(product.Detail); detail != "" {
		b.WriteString("\n简介: ")
		b.WriteString(truncateRunes(detail, detailLimit))
	}
	return b.String()
}

// GetType 返回组件类型标识。
func (l *MallProductLoader) GetType() string {
	return "MallProductLoader"
}

// IsCallbacksEnabled 声明组件自带 callbacks 切面。
func (l *MallProductLoader) IsCallbacksEnabled() bool {
	return true
}

// joinNonEmpty 拼接非空片段并去重相邻重复。
func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == part {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, sep)
}

// loaderTags 组装候选标签：品牌 + 规格原文。
func loaderTags(brand, specs string) []string {
	var tags []string
	if brand = strings.TrimSpace(brand); brand != "" {
		tags = append(tags, brand)
	}
	if specs = strings.TrimSpace(specs); specs != "" {
		tags = append(tags, specs)
	}
	return tags
}

// truncateRunes 按字符截断文本，避免把多字节字符截成半个。
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
