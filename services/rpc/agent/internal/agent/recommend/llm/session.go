package llm

import (
	"sync"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// session 保存单次推荐请求的可变状态。
//
// 每个请求新建一个 session，工具的 Eino handler 通过闭包持有它：
//   - search_products 把候选商品写入缓存，供 select_bundle 按 ID 复用；
//   - select_bundle 把选定套装以类型化的 []BundleItem 直接写回 session；
//   - 所有工具调用记录到 calls，最终回填到响应的 ToolsUsed。
//
// 这样最终结果直接从 session 读取类型化数据，无需再从 Eino 消息流里反解 JSON。
type session struct {
	provider tools.ProductProvider
	selector *selector.BundleSelector
	intent   agentcore.Intent

	mu         sync.Mutex
	candidates []tools.ProductCandidate
	byId       map[string]tools.ProductCandidate
	bundle     []agentcore.BundleItem
	total      int64
	calls      []agentcore.ToolCall
}

// newSession 创建一次推荐请求的会话状态。
func newSession(provider tools.ProductProvider, sel *selector.BundleSelector, intent agentcore.Intent) *session {
	return &session{
		provider: provider,
		selector: sel,
		intent:   intent,
		byId:     make(map[string]tools.ProductCandidate),
	}
}

// storeCandidates 合并式缓存候选商品：同 ID 更新，新 ID 追加。
// 模型可能换关键词多次调用 search_products，合并保证先前搜索的候选 ID 依然可选。
func (s *session) storeCandidates(products []tools.ProductCandidate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, product := range products {
		if _, ok := s.byId[product.Id]; ok {
			for i := range s.candidates {
				if s.candidates[i].Id == product.Id {
					s.candidates[i] = product
					break
				}
			}
		} else {
			s.candidates = append(s.candidates, product)
		}
		s.byId[product.Id] = product
	}
}

// filterCandidates 按 ID 列表从缓存中筛选候选；ids 为空时返回全部缓存候选。
func (s *session) filterCandidates(ids []string) []tools.ProductCandidate {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return append([]tools.ProductCandidate(nil), s.candidates...)
	}
	out := make([]tools.ProductCandidate, 0, len(ids))
	for _, id := range ids {
		if candidate, ok := s.byId[id]; ok {
			out = append(out, candidate)
		}
	}
	return out
}

// hasCandidates 返回当前是否已有缓存候选。
func (s *session) hasCandidates() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.candidates) > 0
}

// setBundle 写回最终选定的商品套装与总价。
func (s *session) setBundle(items []agentcore.BundleItem, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bundle = append([]agentcore.BundleItem(nil), items...)
	s.total = total
}

// recordCall 追加一条工具调用记录。
func (s *session) recordCall(call agentcore.ToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

// snapshot 返回会话累计的套装、总价与工具调用记录的快照。
func (s *session) snapshot() ([]agentcore.BundleItem, int64, []agentcore.ToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agentcore.BundleItem(nil), s.bundle...),
		s.total,
		append([]agentcore.ToolCall(nil), s.calls...)
}
