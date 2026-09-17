package agent

import (
	"fmt"
	"math"
	"strings"
)

const (
	MaxBudgetCents  int64 = 100_000_000_000
	MaxItems        int32 = 10
	MaxQueryRunes         = 2000
	MaxKeywords           = 32
	MaxKeywordRunes       = 128
	MaxCandidateIDs       = 256
)

// Constraints 保存已由服务端解析的有效硬约束。0 只用于外部输入的“缺省”，
// 不得进入选择或最终校验，否则会被误当成不限预算/件数。
type Constraints struct {
	BudgetCents int64
	MaxItems    int32
}

// NewConstraints 检查已解析的预算与件数，所有运行路径使用相同边界。
func NewConstraints(intent Intent) (Constraints, error) {
	if intent.BudgetCents <= 0 || intent.BudgetCents > MaxBudgetCents {
		return Constraints{}, fmt.Errorf("%w: resolved budget is outside 1..%d", ErrInvalidInput, MaxBudgetCents)
	}
	if intent.MaxItems <= 0 || intent.MaxItems > MaxItems {
		return Constraints{}, fmt.Errorf("%w: resolved max_items is outside 1..%d", ErrInvalidInput, MaxItems)
	}
	return Constraints{BudgetCents: intent.BudgetCents, MaxItems: intent.MaxItems}, nil
}

// Restrict 允许模型收紧约束；0 继承，负数拒绝，更大正数收紧到服务端上限。
func (c Constraints) Restrict(budget int64, count int32) (Constraints, bool, error) {
	if _, err := NewConstraints(Intent{BudgetCents: c.BudgetCents, MaxItems: c.MaxItems}); err != nil {
		return Constraints{}, false, err
	}
	if budget < 0 || count < 0 {
		return Constraints{}, false, fmt.Errorf("%w: negative tool constraint", ErrInvalidInput)
	}
	adjusted := budget > c.BudgetCents || count > c.MaxItems
	if budget > 0 {
		c.BudgetCents = min(c.BudgetCents, budget)
	}
	if count > 0 {
		c.MaxItems = min(c.MaxItems, count)
	}
	return c, adjusted, nil
}

// NormalizeCandidates 按 SKU 合并最新快照后再过滤非法/无货商品。
// 不能先过滤：同一 SKU 后来的缺货或非法快照必须使旧的可用快照失效。
// 返回独立切片，不修改数据源或调用方持有的候选。
func NormalizeCandidates(candidates []ProductCandidate) []ProductCandidate {
	order := make([]string, 0, len(candidates))
	latest := make(map[string]ProductCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate.Id == "" || strings.TrimSpace(candidate.Id) != candidate.Id {
			continue
		}
		if _, seen := latest[candidate.Id]; !seen {
			order = append(order, candidate.Id)
		}
		latest[candidate.Id] = candidate
	}
	valid := make([]ProductCandidate, 0, len(order))
	for _, id := range order {
		candidate := latest[id]
		if candidate.PriceCents <= 0 || candidate.PriceCents > MaxBudgetCents || candidate.Stock <= 0 {
			continue
		}
		candidate.Tags = append([]string(nil), candidate.Tags...)
		valid = append(valid, candidate)
	}
	return valid
}

// ValidateResult 以本轮数据源候选为证据校验完整响应，不信任 Result 自带的预算/总价。
// 空结果允许返回，但总价必须为零；有条目时必须有对应的合法商品快照。
func (c Constraints) ValidateResult(result *Result) error {
	if _, err := NewConstraints(Intent{BudgetCents: c.BudgetCents, MaxItems: c.MaxItems}); err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("%w: nil result", ErrUnsafeResult)
	}
	if len(result.Items) > int(c.MaxItems) {
		return fmt.Errorf("%w: too many items", ErrUnsafeResult)
	}
	evidence := make(map[string]ProductCandidate, len(result.Candidates))
	for _, candidate := range NormalizeCandidates(result.Candidates) {
		evidence[candidate.Id] = candidate
	}
	seen := make(map[string]struct{}, len(result.Items))
	var total int64
	for _, item := range result.Items {
		candidate, exists := evidence[item.Id]
		if !exists {
			return fmt.Errorf("%w: item not in valid candidate set", ErrUnsafeResult)
		}
		if _, duplicate := seen[item.Id]; duplicate {
			return fmt.Errorf("%w: duplicate SKU", ErrUnsafeResult)
		}
		seen[item.Id] = struct{}{}
		if item.PriceCents != candidate.PriceCents || item.Stock != candidate.Stock ||
			item.Name != candidate.Name || item.Category != candidate.Category || item.Source != candidate.Source {
			return fmt.Errorf("%w: item differs from candidate snapshot", ErrUnsafeResult)
		}
		if math.IsNaN(item.Score) || math.IsInf(item.Score, 0) {
			return fmt.Errorf("%w: invalid score", ErrUnsafeResult)
		}
		// 用剩余预算比较，避免 total+price 的整数溢出。
		if item.PriceCents > c.BudgetCents-total {
			return fmt.Errorf("%w: budget exceeded", ErrUnsafeResult)
		}
		total += item.PriceCents
	}
	if total != result.TotalPriceCents {
		return fmt.Errorf("%w: incorrect total", ErrUnsafeResult)
	}
	return nil
}

// BundleSummary 只描述确定的商品数量、金额与预算，不复用未经核验的模型价格承诺。
func BundleSummary(count int, total, budget int64) string {
	if count == 0 {
		return "当前候选商品中未生成可用的推荐组合。"
	}
	return fmt.Sprintf("已选择 %d 件商品，总价 %d.%02d 元，预算 %d.%02d 元。",
		count, total/100, total%100, budget/100, budget%100)
}
