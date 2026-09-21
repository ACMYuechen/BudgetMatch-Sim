package eval

import (
	"context"
	"math/bits"
	"slices"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
)

type DemandOracle struct {
	Feasible   bool     `json:"feasible"`
	BestIDs    []string `json:"best_ids"`
	Subsets    int      `json:"enumerated_subsets"`
	Optional   int      `json:"optional_covered"`
	PriceCents int64    `json:"price_cents"`
}

// Independent subset enumeration: no production normalization, preparation,
// objective comparator or AssessSelection. It certifies ONLY this <=8 SKU set.
func demandOracle(ctx context.Context, s demand.State, products []DemandProduct) (DemandOracle, error) {
	out := DemandOracle{BestIDs: []string{}}
	products = slices.Clone(products)
	slices.SortFunc(products, func(a, b DemandProduct) int { return compareString(a.ID, b.ID) })
	var bestRank float64
	for mask := 1; mask < 1<<len(products); mask++ {
		if err := ctx.Err(); err != nil {
			return DemandOracle{}, err
		}
		out.Subsets++
		if bits.OnesCount(uint(mask)) > int(s.MaxItems) {
			continue
		}
		ids := []string{}
		covered := map[demand.Category]bool{}
		price, rank, valid := int64(0), float64(0), true
		for i, p := range products {
			if mask&(1<<i) == 0 {
				continue
			}
			if p.PriceCents <= 0 || p.Stock <= 0 || slices.Contains(s.Excluded, p.Category) || p.Category == demand.Unknown && len(s.Excluded) > 0 {
				valid = false
			}
			price += p.PriceCents // <=8 * validated MaxBudgetCents; cannot overflow.
			var skuRank float64
			if p.KeywordRank > 0 {
				skuRank += 1 / float64(60+p.KeywordRank)
			}
			if p.VectorRank > 0 {
				skuRank += 1 / float64(60+p.VectorRank)
			}
			rank += skuRank
			covered[p.Category] = true
			ids = append(ids, p.ID)
		}
		for _, c := range s.Required {
			valid = valid && covered[c]
		}
		if !valid || price > s.BudgetCents {
			continue
		}
		optional := 0
		for _, c := range s.Optional {
			if covered[c] {
				optional++
			}
		}
		better := !out.Feasible
		if out.Feasible {
			switch {
			case optional != out.Optional:
				better = optional > out.Optional
			case slices.Contains(s.Preferences, demand.Preference("value")) && price != out.PriceCents:
				better = price < out.PriceCents
			case rank != bestRank:
				better = rank > bestRank
			case price != out.PriceCents:
				better = price < out.PriceCents
			case len(ids) != len(out.BestIDs):
				better = len(ids) < len(out.BestIDs)
			default:
				better = slices.Compare(ids, out.BestIDs) < 0
			}
		}
		if better {
			out.Feasible, out.BestIDs, out.Optional, out.PriceCents, bestRank = true, ids, optional, price, rank
		}
	}
	return out, nil
}

func compareString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

type DemandAssessment struct {
	SelectedIDs   []string `json:"selected_ids"`
	TotalCents    int64    `json:"total_price_cents"`
	RequiredMet   int      `json:"required_met"`
	RequiredTotal int      `json:"required_total"`
	OptionalMet   int      `json:"optional_met"`
	OptionalTotal int      `json:"optional_total"`
	TaskSuccess   bool     `json:"task_success"`
	Violations    []string `json:"violations"`
}

// Cross-check returned item facts and sums against fixture-owned facts, never
// against the result's candidate list, self-assessment or explanation metadata.
func assessDemand(s demand.State, products []DemandProduct, items []agent.BundleItem, total int64) DemandAssessment {
	a := DemandAssessment{SelectedIDs: []string{}, TotalCents: total, RequiredTotal: len(s.Required), OptionalTotal: len(s.Optional), Violations: []string{}}
	violate := func(v string) {
		if !slices.Contains(a.Violations, v) {
			a.Violations = append(a.Violations, v)
		}
	}
	byID := map[string]DemandProduct{}
	for _, p := range products {
		byID[p.ID] = p
	}
	seen := map[string]bool{}
	covered := map[demand.Category]bool{}
	var sum int64
	if len(items) > int(s.MaxItems) {
		violate("item_limit")
	}
	for _, item := range items {
		a.SelectedIDs = append(a.SelectedIDs, item.Id)
		p, found := byID[item.Id]
		if seen[item.Id] {
			violate("duplicate_sku")
		}
		seen[item.Id] = true
		if !found || p.Stock <= 0 || p.PriceCents <= 0 || item.PriceCents != p.PriceCents || item.Stock != p.Stock ||
			item.Name != "fixture "+p.ID || item.Category != string(p.Category) || item.Source != "mall" {
			violate("snapshot_fact_mismatch")
			continue
		}
		if item.PriceCents > agent.MaxBudgetCents-sum {
			violate("invalid_total")
		} else {
			sum += item.PriceCents
		}
		if slices.Contains(s.Excluded, p.Category) {
			violate("excluded_category")
		}
		if p.Category == demand.Unknown && len(s.Excluded) > 0 {
			violate("unknown_with_exclusions")
		}
		covered[p.Category] = true
	}
	if total != sum {
		violate("incorrect_total")
	}
	if total > s.BudgetCents {
		violate("budget_limit")
	}
	// Illegal facts/exclusions do not earn coverage. Partial valid coverage is
	// still reported, but missing required categories are hard violations below.
	if len(a.Violations) == 0 {
		for _, c := range s.Required {
			if covered[c] {
				a.RequiredMet++
			}
		}
		for _, c := range s.Optional {
			if covered[c] {
				a.OptionalMet++
			}
		}
	}
	if len(items) > 0 {
		for _, required := range s.Required {
			if !covered[required] {
				violate("missing_required")
				break
			}
		}
	}
	a.TaskSuccess = len(items) > 0 && len(a.Violations) == 0
	slices.Sort(a.SelectedIDs)
	return a
}
