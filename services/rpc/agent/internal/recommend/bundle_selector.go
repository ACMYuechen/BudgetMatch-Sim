package recommend

import (
	"fmt"
	"sort"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

type BundleSelector struct{}

func NewBundleSelector() *BundleSelector {
	return &BundleSelector{}
}

func (s *BundleSelector) Select(candidates []tools.ProductCandidate, intent agent.Intent) ([]agent.BundleItem, int64) {
	maxItems := intent.MaxItems
	if maxItems <= 0 {
		maxItems = 3
	}

	budget := intent.BudgetCents
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

func reason(candidate tools.ProductCandidate, score float64) string {
	return fmt.Sprintf("mock candidate with stock %d, sold %d, score %.2f", candidate.Stock, candidate.Sold, score)
}
