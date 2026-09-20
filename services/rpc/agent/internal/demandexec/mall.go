package demandexec

import (
	"context"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/recommend/beam"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

// NewMall uses a dedicated bounded keyword source, not the legacy unbounded
// SearchProducts fallback or an LLM. Construction performs no network I/O.
func NewMall(provider tools.RankedProductProvider, client tools.CandidateCheckClient) (*Executor, error) {
	if provider == nil || client == nil {
		return nil, agent.ErrInvalidInput
	}
	selector, err := beam.New(beam.Config{MaxCandidates: candidatecontract.MaxCandidates})
	if err != nil {
		return nil, err
	}
	return &Executor{source: &mallSource{provider: provider, verifier: tools.NewMallDemandVerifier(client)}, search: selector, mall: true}, nil
}

// NewMallRetrieval explicitly admits vector/RRF evidence from the configured
// bounded provider. Every category, price and stock fact still comes from Mall.
func NewMallRetrieval(provider tools.ProductProvider, client tools.CandidateCheckClient) (*Executor, error) {
	if provider == nil || client == nil {
		return nil, agent.ErrInvalidInput
	}
	selector, err := beam.New(beam.Config{MaxCandidates: candidatecontract.MaxCandidates})
	if err != nil {
		return nil, err
	}
	return &Executor{source: &mallSource{retrieval: provider, verifier: tools.NewMallDemandVerifier(client)},
		search: selector, mall: true, mallRetrieval: true}, nil
}

type mallSource struct {
	provider  tools.RankedProductProvider
	retrieval tools.ProductProvider
	verifier  *tools.MallCandidateVerifier
}

// Terms are retrieval hints, NEVER category proof. Required terms have priority;
// Mall's existing 4-term / 12-call / 32-result bounds can still miss a category.
func mallSearchRequest(intent agent.Intent) tools.SearchProductsReq {
	keywords := map[string]string{"keyboard": "键盘", "mouse": "鼠标", "lighting": "灯", "stationery": "文具",
		"monitor": "显示器", "headphones": "耳机", "tablet": "平板", "phone": "手机", "computer": "电脑", "accessories": "配件"}
	var terms []string
	if intent.Demand != nil {
		for _, category := range intent.Demand.Required {
			terms = append(terms, keywords[category])
		}
		for _, category := range intent.Demand.Optional {
			terms = append(terms, keywords[category])
		}
	}
	terms = append(terms, intent.Keywords...)
	return tools.SearchProductsReq{Query: strings.Join(intent.Keywords, " "), Keywords: terms,
		BudgetCents: intent.BudgetCents, MaxItems: intent.MaxItems}
}

func mallRetrievalRequest(intent agent.Intent) tools.SearchProductsReq {
	req := mallSearchRequest(intent)
	seen := map[string]bool{}
	terms := make([]string, 0, min(len(req.Keywords), agent.MaxKeywords))
	for _, term := range req.Keywords {
		term = strings.TrimSpace(term)
		if term != "" && !seen[term] && len(terms) < agent.MaxKeywords {
			terms = append(terms, term)
			seen[term] = true
		}
	}
	req.Keywords = terms
	query := []rune(strings.Join(terms, " "))
	req.Query = string(query[:min(len(query), agent.MaxQueryRunes)])
	return req
}

func (s *mallSource) Search(ctx context.Context, intent agent.Intent) ([]agent.ProductCandidate, error) {
	var raw []agent.ProductCandidate
	var err error
	if s.retrieval != nil {
		raw, err = s.retrieval.SearchProducts(ctx, mallRetrievalRequest(intent))
	} else {
		raw, err = s.provider.SearchRankedProducts(ctx, mallSearchRequest(intent), candidatecontract.MaxCandidates)
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > candidatecontract.MaxCandidates || !boundedBatch(Batch{Candidates: raw}) {
		return nil, agent.ErrUnsafeResult
	}
	// Read the COMPLETE bounded response before deduplicating. Conflicting
	// identities are an error, not permission to revive an earlier copy.
	positions := map[string]int{}
	candidates := make([]agent.ProductCandidate, 0, len(raw))
	for _, c := range raw {
		allowedSource := c.Evidence.Source == agent.RetrievalMallKeyword || s.retrieval != nil && c.Evidence.Source == agent.RetrievalMallVector
		if !allowedSource || c.Evidence.State == agent.VerificationDemo || !candidatecontract.ValidID(c.Evidence.ProductID) {
			return nil, agent.ErrUnsafeResult
		}
		c = cloneCandidates([]agent.ProductCandidate{c})[0]
		c.Category, c.Evidence.DemandCategory = "", agent.DemandCategoryEvidence{}
		if index, exists := positions[c.Id]; exists {
			if candidates[index].Evidence.ProductID != c.Evidence.ProductID {
				return nil, agent.ErrUnsafeResult
			}
			candidates[index] = c
		} else {
			positions[c.Id] = len(candidates)
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		return candidates, nil
	}
	// Hydrate category and current purchasability BEFORE the first selection.
	batch, err := s.verifier.Verify(ctx, candidates)
	return batch.Candidates, err
}

func (s *mallSource) Recheck(ctx context.Context, window []agent.ProductCandidate) (Batch, error) {
	batch, err := s.verifier.Verify(ctx, window)
	return Batch{Candidates: batch.Candidates, Unavailable: batch.Unavailable, CheckedAtMs: batch.CheckedAtUnixMs}, err
}

func (e *Executor) catalogFor(candidates []agent.ProductCandidate) (*demand.Catalog, error) {
	if e.mall {
		return demand.NewMallCatalog(candidates)
	}
	return e.catalog, nil
}
