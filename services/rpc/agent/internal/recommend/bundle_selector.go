// Package recommend 提供基于意图的商品组合选择逻辑。
package recommend

import (
	"fmt"
	"sort"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// BundleSelector 根据用户意图从候选商品中选择最优商品组合。
type BundleSelector struct{}

// NewBundleSelector 创建一个新的 BundleSelector 实例。
func NewBundleSelector() *BundleSelector {
	return &BundleSelector{}
}

// Select 按评分排序并选择不超过预算与数量的商品组合，返回商品列表与总价。
func (s *BundleSelector) Select(candidates []tools.ProductCandidate, intent agent.Intent) ([]agent.BundleItem, int64) {
	// 若未指定最大商品数，则使用默认值 3。
	maxItems := intent.MaxItems
	if maxItems <= 0 {
		maxItems = 3
	}

	budget := intent.BudgetCents
	// 按综合评分降序排序，优先选择得分高的商品。
	sort.SliceStable(candidates, func(i, j int) bool {
		return score(candidates[i], budget) > score(candidates[j], budget)
	})

	var total int64
	items := make([]agent.BundleItem, 0, maxItems)
	for _, candidate := range candidates {
		if candidate.Stock <= 0 {
			continue
		}
		if budget > 0 && total+candidate.PriceCents > budget {
			continue
		}
		itemScore := score(candidate, budget)
		items = append(items, agent.BundleItem{
			ID:         candidate.ID,
			Name:       candidate.Name,
			Category:   candidate.Category,
			Source:     candidate.Source,
			PriceCents: candidate.PriceCents,
			Stock:      candidate.Stock,
			Score:      itemScore,
			Reason:     reason(candidate, itemScore),
		})
		total += candidate.PriceCents
		if int32(len(items)) >= maxItems {
			break
		}
	}

	return items, total
}

// score 根据性价比、销量、库存与预算匹配度计算候选商品得分。
func score(candidate tools.ProductCandidate, budget int64) float64 {
	valueScore := 100000.0 / float64(candidate.PriceCents+1)
	soldScore := float64(candidate.Sold) / 100.0
	stockScore := float64(candidate.Stock) / 20.0
	budgetScore := 0.0
	if budget > 0 {
		budgetScore = 20.0 * (1.0 - float64(candidate.PriceCents)/float64(budget))
		if budgetScore < 0 {
			budgetScore = 0
		}
	}
	return valueScore + soldScore + stockScore + budgetScore
}

// reason 生成选中商品的推荐理由说明。
func reason(candidate tools.ProductCandidate, score float64) string {
	return fmt.Sprintf("%s candidate with stock %d, sold %d, score %.2f", candidate.Source, candidate.Stock, candidate.Sold, score)
}
