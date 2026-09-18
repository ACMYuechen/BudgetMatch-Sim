package tools

import (
	"context"
	"math"
	"strings"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/internal/safety"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ragMinTopK 是向量检索的最小召回数，保证选择器有足够候选。
const ragMinTopK = 8

// RAGProductProvider 是 ProductProvider 的语义检索实现：
// 优先走 pgvector 余弦检索，从向量表的业务快照还原候选；
// 技术错误或空结果回退关键词；取消/鉴权失败终止。最终实时校验由 Service 统一执行。
type RAGProductProvider struct {
	retriever retriever.Retriever
	fallback  ProductProvider
	topK      int
}

// 确保 RAGProductProvider 实现 ProductProvider。
var _ ProductProvider = (*RAGProductProvider)(nil)

// NewRAGProductProvider 创建语义检索 provider。fallback 必填。
func NewRAGProductProvider(r retriever.Retriever, fallback ProductProvider, topK int) *RAGProductProvider {
	if topK < ragMinTopK {
		topK = ragMinTopK
	}
	return &RAGProductProvider{
		retriever: r,
		fallback:  fallback,
		topK:      topK,
	}
}

// Name 返回该提供者的标识名称。
func (p *RAGProductProvider) Name() string {
	return "rag.pgvector"
}

// SearchProducts 语义检索候选商品，失败或为空时回退关键词链路。
func (p *RAGProductProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.retriever == nil || p.fallback == nil {
		return nil, status.Error(codes.Unavailable, "retrieval provider unavailable")
	}
	query := buildSemanticQuery(req)
	docs, err := p.retriever.Retrieve(ctx, query, retriever.WithTopK(p.retrieveTopK(req.MaxItems)))
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err != nil {
		if agentcore.IsExecutionStopped(err) {
			return nil, err
		}
		logx.WithContext(ctx).Errorw("rag retrieval failed, falling back to keyword provider",
			logx.Field("provider", safety.Label(p.fallback.Name())), logx.Field("error_code", safety.ErrorCode(err)))
		return p.fallback.SearchProducts(ctx, req)
	}

	var out []ProductCandidate
	for _, doc := range docs {
		meta, ok := rag.CandidateFromDocument(doc)
		if !ok {
			continue
		}
		candidate := ProductCandidate{
			Evidence: agentcore.CandidateEvidence{Source: agentcore.RetrievalMallVector,
				ProductID: meta.ProductId, SnapshotAtUnixMs: meta.SnapshotAtUnixMs,
				RetrievedAtUnixMs: time.Now().UnixMilli(), Relevance: doc.Score(), HasRelevance: true},
			Id:         doc.ID,
			Name:       meta.Name,
			Category:   meta.Category,
			Source:     "mall+rag", // 标记走了语义检索链路，便于在 tools_used 中辨识
			PriceCents: meta.PriceCents,
			Stock:      meta.Stock,
			Sold:       meta.Sold,
			Tags:       meta.Tags,
		}
		if math.IsNaN(candidate.Evidence.Relevance) || math.IsInf(candidate.Evidence.Relevance, 0) {
			continue
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
		// 首轮同步未完成、阈值过严或快照无货，回退关键词链路。
		logx.WithContext(ctx).Infow("rag retrieval returned no usable candidates, falling back",
			logx.Field("provider", safety.Label(p.fallback.Name())), logx.Field("retrieved", len(docs)))
		return p.fallback.SearchProducts(ctx, req)
	}
	return out, nil
}

// retrieveTopK 依据期望条目数放大召回：候选须经预算/库存过滤，召回按 4 倍冗余。
func (p *RAGProductProvider) retrieveTopK(maxItems int32) int {
	return min(max(int(maxItems)*4, ragMinTopK), p.topK)
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
