package demand

import (
	"slices"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

type Assessment struct {
	ConstraintsSatisfied bool       `json:"constraints_satisfied"`
	TotalPriceCents      int64      `json:"total_price_cents"`
	CoveredRequired      []Category `json:"covered_required"`
	MissingRequired      []Category `json:"missing_required"`
	CoveredOptional      []Category `json:"covered_optional"`
	UnknownSKUs          []string   `json:"unknown_skus"`
	Violations           []string   `json:"violations"`
}

// AssessSelection checks ONE supplied selection against demo category evidence
// and snapshot limits. It neither searches for a bundle nor checks live facts,
// and a failure is NOT proof of global infeasibility. Preferences are unscored.
func AssessSelection(s State, catalog *Catalog, selected []agent.ProductCandidate) (Assessment, error) {
	if s.Validate() != nil || catalog == nil || catalog.sha256 == "" || len(selected) > agent.MaxCandidateIDs {
		return Assessment{}, ErrInvalid
	}
	s = canonical(s)
	out := Assessment{CoveredRequired: []Category{}, MissingRequired: []Category{}, CoveredOptional: []Category{}, UnknownSKUs: []string{}, Violations: []string{}}
	violations := map[string]bool{}
	seen := map[string]bool{}
	covered := map[Category]bool{}
	if len(selected) > int(s.MaxItems) {
		violations["too_many_items"] = true
	}
	for _, candidate := range selected {
		if !candidatecontract.ValidID(candidate.Id) || !candidatecontract.ValidID(candidate.Evidence.ProductID) ||
			candidate.PriceCents <= 0 || candidate.PriceCents > agent.MaxBudgetCents || candidate.Stock <= 0 {
			violations["invalid_candidate"] = true
			continue
		}
		if seen[candidate.Id] {
			violations["duplicate_sku"] = true
			continue
		}
		seen[candidate.Id] = true
		// At most 256 prices, each <= MaxBudgetCents; the sum cannot overflow.
		out.TotalPriceCents += candidate.PriceCents
		category := catalog.Classify(candidate).Category
		if category == Unknown {
			out.UnknownSKUs = append(out.UnknownSKUs, candidate.Id)
			// Unknown cannot prove absence of a hard-excluded category.
			if len(s.Excluded) > 0 {
				violations["unknown_category_with_exclusions"] = true
			}
			continue
		}
		if slices.Contains(s.Excluded, category) {
			violations["excluded_category"] = true
		}
		covered[category] = true
	}
	if out.TotalPriceCents > s.BudgetCents {
		violations["over_budget"] = true
	}
	for _, category := range s.Required {
		if covered[category] {
			out.CoveredRequired = append(out.CoveredRequired, category)
		} else {
			out.MissingRequired = append(out.MissingRequired, category)
		}
	}
	for _, category := range s.Optional {
		if covered[category] {
			out.CoveredOptional = append(out.CoveredOptional, category)
		}
	}
	for code := range violations {
		out.Violations = append(out.Violations, code)
	}
	slices.Sort(out.Violations)
	slices.Sort(out.UnknownSKUs)
	out.ConstraintsSatisfied = len(out.MissingRequired) == 0 && len(out.Violations) == 0
	return out, nil
}
