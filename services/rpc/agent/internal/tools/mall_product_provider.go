package tools

import (
	"context"
	"encoding/json"
	"strings"

	"budgetmatch-sim/services/rpc/mall/pb"

	"google.golang.org/grpc"
)

// mall 商品检索的分页与候选上限。
// 在线工具调用要求低延迟，这里刻意封顶拉取量；全量拉取只发生在离线向量同步链路，
// 两条链路的分页策略分开设计。
const (
	mallStatusOnShelf   = 1  // 上架状态
	mallProductPageSize = 20 // 商品分页大小
	mallMaxPagesPerTerm = 3  // 每个搜索词最多翻页数
	mallMaxProducts     = 30 // 单次检索的候选商品上限
	mallSkuPageSize     = 50 // SKU 分页大小
	mallMaxSkuPages     = 5  // 单商品 SKU 最多翻页数
)

// productSearcher 是 provider 依赖的 mall 商品能力子集，
// 生成的客户端 productservice.ProductService 天然满足，测试中可用轻量 stub 替代。
type productSearcher interface {
	ListProducts(ctx context.Context, in *pb.ListProductsReq, opts ...grpc.CallOption) (*pb.ListProductsResp, error)
	ListSkusByProduct(ctx context.Context, in *pb.ListSkusByProductReq, opts ...grpc.CallOption) (*pb.ListSkusByProductResp, error)
}

// MallProductProvider 是 ProductProvider 的 mall-rpc 实现：
// 用关键词逐个调用 ListProducts 收集上架商品，再展开各商品的上架 SKU 作为候选。
// 每个 SKU 是一个候选（价格/库存/销量在 SKU 上），ID 即 mall 的 SKU ID，可直接下单。
type MallProductProvider struct {
	client productSearcher
}

// 确保 MallProductProvider 实现 ProductProvider。
var _ ProductProvider = (*MallProductProvider)(nil)

// NewMallProductProvider 基于 mall 商品服务客户端创建 provider。
func NewMallProductProvider(client productSearcher) *MallProductProvider {
	return &MallProductProvider{client: client}
}

// Name 返回该提供者的标识名称。
func (p *MallProductProvider) Name() string {
	return "mall.product_provider"
}

// SearchProducts 按关键词检索 mall 上架商品并展开 SKU，最后按库存与预算过滤。
func (p *MallProductProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	products, err := p.collectProducts(ctx, searchTerms(req))
	if err != nil {
		return nil, err
	}

	var out []ProductCandidate
	for _, product := range products {
		skus, err := p.collectSkus(ctx, product.Id)
		if err != nil {
			return nil, err
		}
		for _, sku := range skus {
			if sku.Stock <= 0 {
				continue
			}
			if req.BudgetCents > 0 && sku.Price > req.BudgetCents {
				continue
			}
			out = append(out, candidateFromSku(product, sku))
		}
	}
	return out, nil
}

// searchTerms 归并搜索词：解析出的关键词优先（命中率高），原始查询兜底，去重去空。
func searchTerms(req SearchProductsReq) []string {
	seen := make(map[string]struct{})
	var terms []string
	add := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" {
			return
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		terms = append(terms, term)
	}
	for _, keyword := range req.Keywords {
		add(keyword)
	}
	add(req.Query)
	return terms
}

// collectProducts 对每个搜索词分页拉取上架商品，按商品 ID 去重，达到候选上限即停。
func (p *MallProductProvider) collectProducts(ctx context.Context, terms []string) ([]*pb.Product, error) {
	seen := make(map[string]struct{})
	var products []*pb.Product

	for _, term := range terms {
		for page := int32(1); page <= mallMaxPagesPerTerm; page++ {
			if len(products) >= mallMaxProducts {
				return products, nil
			}
			resp, err := p.client.ListProducts(ctx, &pb.ListProductsReq{
				Page:     page,
				PageSize: mallProductPageSize,
				Keyword:  term,
				Status:   mallStatusOnShelf,
			})
			if err != nil {
				return nil, err
			}
			for _, product := range resp.GetList() {
				if product == nil {
					continue
				}
				if _, ok := seen[product.Id]; ok {
					continue
				}
				seen[product.Id] = struct{}{}
				products = append(products, product)
				if len(products) >= mallMaxProducts {
					return products, nil
				}
			}
			if int64(page)*int64(mallProductPageSize) >= resp.GetTotal() || len(resp.GetList()) == 0 {
				break
			}
		}
	}
	return products, nil
}

// collectSkus 分页拉取商品的全部上架 SKU。
func (p *MallProductProvider) collectSkus(ctx context.Context, productID string) ([]*pb.Sku, error) {
	var skus []*pb.Sku
	for page := int32(1); page <= mallMaxSkuPages; page++ {
		resp, err := p.client.ListSkusByProduct(ctx, &pb.ListSkusByProductReq{
			ProductId: productID,
			Page:      page,
			PageSize:  mallSkuPageSize,
			Status:    mallStatusOnShelf,
		})
		if err != nil {
			return nil, err
		}
		skus = append(skus, resp.GetList()...)
		if int64(page)*int64(mallSkuPageSize) >= resp.GetTotal() || len(resp.GetList()) == 0 {
			break
		}
	}
	return skus, nil
}

// candidateFromSku 把商品与 SKU 映射为候选：名称拼接 SPU+SKU，标签取供应商与规格值。
func candidateFromSku(product *pb.Product, sku *pb.Sku) ProductCandidate {
	return ProductCandidate{
		ID:         sku.Id,
		Name:       joinName(product.Name, sku.Name),
		Source:     "mall",
		PriceCents: sku.Price,
		Stock:      sku.Stock,
		Sold:       sku.Sold,
		Tags:       skuTags(product.Providor, sku.Specs),
	}
}

// joinName 拼接商品名与 SKU 名，SKU 名为空或重复时只保留商品名。
func joinName(productName, skuName string) string {
	productName = strings.TrimSpace(productName)
	skuName = strings.TrimSpace(skuName)
	if skuName == "" || skuName == productName {
		return productName
	}
	if productName == "" {
		return skuName
	}
	return productName + " " + skuName
}

// skuTags 组装候选标签：供应商 + 规格 JSON 中的字符串值（解析失败则忽略规格）。
func skuTags(providor, specs string) []string {
	var tags []string
	if providor = strings.TrimSpace(providor); providor != "" {
		tags = append(tags, providor)
	}
	if specs = strings.TrimSpace(specs); specs != "" {
		var kv map[string]any
		if err := json.Unmarshal([]byte(specs), &kv); err == nil {
			for _, v := range kv {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					tags = append(tags, strings.TrimSpace(s))
				}
			}
		}
	}
	return tags
}
