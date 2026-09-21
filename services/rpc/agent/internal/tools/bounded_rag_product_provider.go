package tools

import (
	"context"
	"fmt"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"

	"github.com/cloudwego/eino/components/retriever"
)

// BoundedRAGProductProvider is the explicit demand-execution vector-first path.
// Unlike the legacy provider, BOTH vector and keyword reads have a fixed window;
// fallback never invokes the old unbounded SearchProducts implementation.
type BoundedRAGProductProvider struct {
	vector  retriever.Retriever
	keyword RankedProductProvider
	limit   int
	timeout time.Duration
}

func NewBoundedRAGProductProvider(vector retriever.Retriever, keyword RankedProductProvider, cfg rag.Config) (*BoundedRAGProductProvider, error) {
	if err := cfg.Retrieval.Validate(cfg.TopK); err != nil {
		return nil, err
	}
	if vector == nil || keyword == nil || cfg.TopK < 0 || cfg.Normalize().TopK > rag.MaxHybridOutput ||
		cfg.Retrieval.Normalize().Strategy != rag.StrategyVectorFirst {
		return nil, fmt.Errorf("bounded vector retrieval requires both sources and TopK within 1..32")
	}
	return &BoundedRAGProductProvider{vector: vector, keyword: keyword, limit: cfg.Normalize().TopK,
		timeout: time.Duration(cfg.Retrieval.Normalize().TimeoutMillis) * time.Millisecond}, nil
}

func (*BoundedRAGProductProvider) Name() string { return "rag.bounded_vector_first" }

func (p *BoundedRAGProductProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validHybridRequest(req); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req.Keywords = append([]string(nil), req.Keywords...)
	// Reuse the strict count/payload/metadata conversion, but do not manufacture
	// RRF scores for this single vector lane.
	reader := HybridProductProvider{vector: p.vector}
	raw, err := reader.vectorCandidates(ctx, req, p.limit)
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err == nil {
		err = validateRanking(raw, 1, p.limit)
	}
	if agentcore.IsExecutionStopped(err) {
		return nil, err
	}
	if err == nil {
		// Duplicate parents are unsafe. Later unavailable facts must invalidate
		// earlier valid snapshots rather than suppressing keyword fallback.
		for _, entry := range indexRanking(raw) {
			if entry.conflicted {
				return nil, agentcore.ErrUnsafeResult
			}
		}
		var candidates []ProductCandidate
		for _, c := range agentcore.NormalizeCandidates(raw) {
			if c.PriceCents <= req.BudgetCents {
				candidates = append(candidates, c)
			}
		}
		if len(candidates) > 0 {
			return candidates, nil
		}
	}
	// At most one bounded fallback. Auth/cancellation/deadline errors above
	// cannot be downgraded to a different source or a successful empty result.
	candidates, err := p.keyword.SearchRankedProducts(ctx, req, p.limit)
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err == nil {
		err = validateRanking(candidates, 0, p.limit)
	}
	if err != nil {
		return nil, err
	}
	return candidates, nil
}
