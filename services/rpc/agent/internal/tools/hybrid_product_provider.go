package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"

	"github.com/cloudwego/eino/components/retriever"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HybridProductProvider is an opt-in experiment. All mutable state (including
// traces) is request-local, so concurrent conversations cannot share evidence.
type HybridProductProvider struct {
	vector  retriever.Retriever
	keyword RankedProductProvider
	cfg     rag.RetrievalConfig
	outputK int
}

func NewHybridProductProvider(vector retriever.Retriever, keyword RankedProductProvider, cfg rag.Config) (*HybridProductProvider, error) {
	if err := cfg.Retrieval.Validate(cfg.TopK); err != nil {
		return nil, err
	}
	cfg = cfg.Normalize()
	if cfg.Retrieval.Normalize().Strategy != rag.StrategyHybridRRF || vector == nil || keyword == nil {
		return nil, fmt.Errorf("hybrid retrieval requires both bounded sources and explicit strategy")
	}
	return &HybridProductProvider{vector: vector, keyword: keyword, cfg: cfg.Retrieval.Normalize(), outputK: cfg.TopK}, nil
}

func (*HybridProductProvider) Name() string { return "rag.hybrid_rrf" }

func (p *HybridProductProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	result, err := p.SearchProductsWithTrace(ctx, req)
	return result.Candidates, err
}

type retrievalLane struct {
	candidates []ProductCandidate
	err        error
	elapsed    time.Duration
}

func (p *HybridProductProvider) SearchProductsWithTrace(ctx context.Context, req SearchProductsReq) (ProductSearch, error) {
	var result ProductSearch
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validHybridRequest(req); err != nil {
		return result, err
	}
	// Copy slices before handing them to concurrent source implementations.
	req.Keywords = append([]string(nil), req.Keywords...)
	budgetCtx, cancel := context.WithTimeout(ctx, time.Duration(p.cfg.TimeoutMillis)*time.Millisecond)
	defer cancel()
	var lanes [2]retrievalLane // keyword then vector, never arrival order
	run := func(window int, enabled [2]bool) error {
		attemptCtx, stop := context.WithTimeout(budgetCtx, time.Duration(p.cfg.TimeoutMillis)*time.Millisecond/2)
		defer stop()
		var wg sync.WaitGroup
		for i := range lanes {
			if !enabled[i] {
				continue
			}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				start := time.Now()
				var candidates []ProductCandidate
				var err error
				local := req
				local.Keywords = append([]string(nil), req.Keywords...)
				if i == 0 {
					candidates, err = p.keyword.SearchRankedProducts(attemptCtx, local, window)
				} else {
					candidates, err = p.vectorCandidates(attemptCtx, local, window)
				}
				if err == nil {
					err = attemptCtx.Err()
				}
				if err == nil {
					err = validateRanking(candidates, i, window)
				}
				if err == nil {
					err = attemptCtx.Err()
				}
				if terminalRetrievalError(err) {
					stop()
				}
				lanes[i] = retrievalLane{candidates: candidates, err: err, elapsed: time.Since(start)}
			}(i)
		}
		// Production dependencies must honor context. Wait for both rather than
		// leaving detached workers to mutate traces after the request returns.
		wg.Wait()
		for i, lane := range lanes {
			if !enabled[i] {
				continue
			}
			name := "retrieval.keyword"
			if i == 1 {
				name = "retrieval.vector"
			}
			call := agentcore.ToolCall{Name: name, Success: lane.err == nil, Detail: fmt.Sprintf("loaded %d candidates", len(lane.candidates))}
			if lane.err != nil {
				call.Detail = fmt.Sprintf("error_code=%s duration_ms=%d", safety.ErrorCode(lane.err), lane.elapsed.Milliseconds())
			}
			result.Calls = append(result.Calls, call)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// An auth denial vetoes both lanes even if cancellation of its sibling
		// wins scheduling. Do not relabel it as a recoverable timeout.
		for _, lane := range lanes {
			if status.Code(lane.err) == codes.Unauthenticated || status.Code(lane.err) == codes.PermissionDenied {
				return safety.Protect(lane.err)
			}
		}
		for _, lane := range lanes {
			if terminalRetrievalError(lane.err) {
				return safety.Protect(lane.err)
			}
		}
		if budgetCtx.Err() != nil {
			return status.Error(codes.Unavailable, "retrieval time budget exhausted")
		}
		return nil
	}
	if err := run(p.cfg.InitialK, [2]bool{true, true}); err != nil {
		return result, err
	}
	fuse := func() ([]ProductCandidate, int) {
		var candidates [2][]ProductCandidate
		for i, lane := range lanes {
			if lane.err == nil {
				candidates[i] = lane.candidates
			}
		}
		return fuseRankings(candidates[0], candidates[1], req.BudgetCents, p.outputK)
	}
	recordConflicts := func(conflicts int) {
		if conflicts > 0 {
			result.Calls = append(result.Calls, agentcore.ToolCall{Name: "retrieval.conflict", Success: false, Detail: "status=failed"})
		}
	}
	candidates, conflicts := fuse()
	recordConflicts(conflicts)
	if len(candidates) < p.outputK && p.cfg.MaxK > p.cfg.InitialK && (lanes[0].err == nil || lanes[1].err == nil) {
		result.Calls = append(result.Calls, agentcore.ToolCall{Name: "retrieval.expand", Success: true, Detail: "status=ok"})
		// At most one expansion; failed lanes are never retried. A failed wider
		// read invalidates that lane's earlier response, rather than reviving it.
		if err := run(p.cfg.MaxK, [2]bool{lanes[0].err == nil, lanes[1].err == nil}); err != nil {
			return result, err
		}
		candidates, conflicts = fuse()
		recordConflicts(conflicts)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if budgetCtx.Err() != nil {
		return result, status.Error(codes.Unavailable, "retrieval time budget exhausted")
	}
	if len(candidates) == 0 && (lanes[0].err != nil || lanes[1].err != nil) {
		return result, status.Error(codes.Unavailable, "retrieval sources unavailable")
	}
	result.Candidates = candidates
	result.Calls = append(result.Calls, agentcore.ToolCall{Name: "retrieval.fusion", Success: true, Detail: fmt.Sprintf("loaded %d candidates", len(candidates))})
	return result, nil
}

func terminalRetrievalError(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled ||
		status.Code(err) == codes.PermissionDenied || status.Code(err) == codes.Unauthenticated)
}

func validHybridRequest(req SearchProductsReq) error {
	if _, err := agentcore.NewConstraints(agentcore.Intent{BudgetCents: req.BudgetCents, MaxItems: req.MaxItems}); err != nil {
		return err
	}
	if strings.TrimSpace(req.Query) == "" || utf8.RuneCountInString(req.Query) > agentcore.MaxQueryRunes || len(req.Keywords) > agentcore.MaxKeywords {
		return agentcore.ErrInvalidInput
	}
	for _, keyword := range req.Keywords {
		if utf8.RuneCountInString(keyword) > agentcore.MaxKeywordRunes {
			return agentcore.ErrInvalidInput
		}
	}
	return nil
}

func validateRanking(candidates []ProductCandidate, lane, limit int) error {
	if len(candidates) > limit {
		return status.Error(codes.Unavailable, "retrieval window exceeded")
	}
	source := agentcore.RetrievalMallKeyword
	if lane == 1 {
		source = agentcore.RetrievalMallVector
	}
	size := 0
	for _, candidate := range candidates {
		if !candidatecontract.ValidID(candidate.Id) || !candidatecontract.ValidID(candidate.Evidence.ProductID) ||
			candidate.Evidence.Source != source || candidate.Evidence.State == agentcore.VerificationDemo ||
			(candidate.Evidence.HasRelevance && (math.IsNaN(candidate.Evidence.Relevance) || math.IsInf(candidate.Evidence.Relevance, 0))) {
			return status.Error(codes.Unavailable, "invalid retrieval evidence")
		}
		encoded, err := json.Marshal(candidate)
		if err != nil {
			return status.Error(codes.Unavailable, "invalid retrieval snapshot")
		}
		size += len(encoded)
		if size > maxRankedResponseBytes {
			return status.Error(codes.Unavailable, "retrieval snapshot payload exceeded")
		}
	}
	return nil
}

func (p *HybridProductProvider) vectorCandidates(ctx context.Context, req SearchProductsReq, limit int) ([]ProductCandidate, error) {
	docs, err := p.vector.Retrieve(ctx, buildSemanticQuery(req), retriever.WithTopK(limit))
	if err != nil {
		return nil, err
	}
	if len(docs) > limit {
		return nil, status.Error(codes.Unavailable, "vector window exceeded")
	}
	var out []ProductCandidate
	size := 0
	for _, doc := range docs {
		meta, ok := rag.CandidateFromDocument(doc)
		if !ok {
			return nil, status.Error(codes.Unavailable, "invalid vector snapshot")
		}
		encoded, err := json.Marshal(meta)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "invalid vector snapshot")
		}
		size += len(doc.ID) + len(doc.Content) + len(encoded)
		if size > maxRankedResponseBytes {
			return nil, status.Error(codes.Unavailable, "vector snapshot payload exceeded")
		}
		out = append(out, ProductCandidate{Id: doc.ID, Name: meta.Name, Category: meta.Category, Source: "mall+rag",
			PriceCents: meta.PriceCents, Stock: meta.Stock, Sold: meta.Sold, Tags: append([]string(nil), meta.Tags...),
			Evidence: agentcore.CandidateEvidence{Source: agentcore.RetrievalMallVector, ProductID: meta.ProductId,
				Relevance: doc.Score(), HasRelevance: true, SnapshotAtUnixMs: meta.SnapshotAtUnixMs, RetrievedAtUnixMs: time.Now().UnixMilli()}})
	}
	return out, nil
}
