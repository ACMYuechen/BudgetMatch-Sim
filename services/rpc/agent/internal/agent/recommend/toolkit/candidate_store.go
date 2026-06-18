package toolkit

import (
	"context"
	"sync"

	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

type candidateStore struct {
	provider tools.ProductProvider

	mu         sync.RWMutex
	candidates []tools.ProductCandidate
	byID       map[string]tools.ProductCandidate
}

func newCandidateStore(provider tools.ProductProvider) *candidateStore {
	return &candidateStore{
		provider: provider,
		byID:     make(map[string]tools.ProductCandidate),
	}
}

func (s *candidateStore) Name() string {
	if s.provider == nil {
		return ""
	}
	return s.provider.Name()
}

func (s *candidateStore) SearchProducts(ctx context.Context, req tools.SearchProductsReq) ([]tools.ProductCandidate, error) {
	products, err := s.provider.SearchProducts(ctx, req)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.candidates = append([]tools.ProductCandidate(nil), products...)
	s.byID = make(map[string]tools.ProductCandidate, len(products))
	for _, product := range products {
		s.byID[product.ID] = product
	}
	return products, nil
}

func (s *candidateStore) Filter(ids []string) []tools.ProductCandidate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(ids) == 0 {
		return append([]tools.ProductCandidate(nil), s.candidates...)
	}

	out := make([]tools.ProductCandidate, 0, len(ids))
	for _, id := range ids {
		if candidate, ok := s.byID[id]; ok {
			out = append(out, candidate)
		}
	}
	return out
}
