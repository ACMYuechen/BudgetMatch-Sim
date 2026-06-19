// Package toolkit 提供推荐 Agent 的工具执行层，包括商品搜索、组合选择及 MCP 工具调用。
package toolkit

import (
	"context"
	"sync"

	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// candidateStore 是商品候选缓存，封装了 ProductProvider 的搜索能力，
// 并在内存中维护候选列表及按 ID 索引，供后续 select_bundle 使用。
type candidateStore struct {
	provider tools.ProductProvider // 底层商品数据源

	mu         sync.RWMutex                      // 保护 candidates 和 byID 的读写锁
	candidates []tools.ProductCandidate          // 当前缓存的商品候选列表
	byID       map[string]tools.ProductCandidate // 按商品 ID 索引的候选映射
}

// newCandidateStore 创建一个新的候选缓存实例。
func newCandidateStore(provider tools.ProductProvider) *candidateStore {
	return &candidateStore{
		provider: provider,
		byID:     make(map[string]tools.ProductCandidate),
	}
}

// Name 返回底层数据源名称；若 provider 为 nil 则返回空字符串。
func (s *candidateStore) Name() string {
	if s.provider == nil {
		return ""
	}
	return s.provider.Name()
}

// SearchProducts 调用底层 provider 搜索商品，并将结果缓存到内存中（覆盖旧缓存）。
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

// Filter 根据商品 ID 列表从缓存中筛选候选商品；若 ids 为空则返回全部缓存候选。
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
