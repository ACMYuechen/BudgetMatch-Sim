package tools

import (
	"context"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/mall/client/productservice"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
)

// ragMinTopK 是向量检索的最小召回数，保证选择器有足够候选。
const ragMinTopK = 8

// SkuGetter 是实时校验所需的 mall 能力子集。
type SkuGetter interface {
	GetSku(ctx context.Context, in *productservice.GetSkuReq, opts ...grpc.CallOption) (*productservice.GetSkuResp, error)
}

// RAGProductProvider 是 ProductProvider 的语义检索实现：
// 优先走 pgvector 余弦检索，从向量表的业务快照还原候选；
// 检索出错或结果为空时回退到关键词 provider（mall/mock），绝不向上抛错中断工具调用。
type RAGProductProvider struct {
	retriever retriever.Retriever
	fallback  ProductProvider
	verify    SkuGetter // verify 非 nil 时对检索结果做 GetSku 实时校验（价格/库存/上架状态）
	topK      int
}

// 确保 RAGProductProvider 实现 ProductProvider。
var _ ProductProvider = (*RAGProductProvider)(nil)

// NewRAGProductProvider 创建语义检索 provider。fallback 必填；verify 可为 nil。
func NewRAGProductProvider(r retriever.Retriever, fallback ProductProvider, verify SkuGetter, topK int) *RAGProductProvider {
	if topK < ragMinTopK {
		topK = ragMinTopK
	}
	return &RAGProductProvider{
		retriever: r,
		fallback:  fallback,
		verify:    verify,
		topK:      topK,
	}
}

// Name 返回该提供者的标识名称。
func (p *RAGProductProvider) Name() string {
	return "rag.pgvector"
}

// SearchProducts 语义检索候选商品，失败或为空时回退关键词链路。
func (p *RAGProductProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	query := buildSemanticQuery(req)
	docs, err := p.retriever.Retrieve(ctx, query, retriever.WithTopK(p.retrieveTopK(req.MaxItems)))
	if err != nil {
		logx.WithContext(ctx).Errorw("rag retrieval failed, falling back to keyword provider",
			logx.Field("provider", p.fallback.Name()), logx.Field("error", err.Error()))
		return p.fallback.SearchProducts(ctx, req)
	}

	var out []ProductCandidate
	for _, doc := range docs {
		meta, ok := rag.CandidateFromDocument(doc)
		if !ok {
			continue
		}
		candidate := ProductCandidate{
			ID:         doc.ID,
			Name:       meta.Name,
			Category:   meta.Category,
			Source:     "mall+rag", // 标记走了语义检索链路，便于在 tools_used 中辨识
			PriceCents: meta.PriceCents,
			Stock:      meta.Stock,
			Sold:       meta.Sold,
			Tags:       meta.Tags,
		}
		if !p.refresh(ctx, &candidate) {
			continue // 实时校验发现已下架
		}
		if candidate.Stock <= 0 {
			continue
		}
		if req.BudgetCents > 0 && candidate.PriceCents > req.BudgetCents {
			continue
		}
		out = append(out, candidate)
	}

	if len(out) == 0 {
		// 首轮同步未完成、阈值过滤过严或校验后无货，都回退关键词链路。
		logx.WithContext(ctx).Infow("rag retrieval returned no usable candidates, falling back",
			logx.Field("provider", p.fallback.Name()), logx.Field("retrieved", len(docs)))
		return p.fallback.SearchProducts(ctx, req)
	}
	return out, nil
}

// retrieveTopK 依据期望条目数放大召回：候选须经预算/库存过滤，召回按 4 倍冗余。
func (p *RAGProductProvider) retrieveTopK(maxItems int32) int {
	return min(max(int(maxItems)*4, ragMinTopK), p.topK)
}

// refresh 用 mall 实时数据刷新候选的价格与库存；返回 false 表示 SKU 已下架应剔除。
// 未配置 verify 或查询失败时保留向量表快照（快照最长滞后一个同步周期）。
func (p *RAGProductProvider) refresh(ctx context.Context, candidate *ProductCandidate) bool {
	if p.verify == nil {
		return true
	}
	resp, err := p.verify.GetSku(ctx, &productservice.GetSkuReq{Id: candidate.ID})
	if err != nil || resp.GetSku() == nil {
		return true
	}
	sku := resp.GetSku()
	if sku.Status != mallStatusOnShelf {
		return false
	}
	candidate.PriceCents = sku.Price
	candidate.Stock = sku.Stock
	candidate.Sold = sku.Sold
	return true
}

// buildSemanticQuery 组装检索文本：原始查询 + 解析关键词。
func buildSemanticQuery(req SearchProductsReq) string {
	parts := make([]string, 0, len(req.Keywords)+1)
	if q := strings.TrimSpace(req.Query); q != "" {
		parts = append(parts, q)
	}
	for _, keyword := range req.Keywords {
		if keyword = strings.TrimSpace(keyword); keyword != "" {
			parts = append(parts, keyword)
		}
	}
	return strings.Join(parts, " ")
}
