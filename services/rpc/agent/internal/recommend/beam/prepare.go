package beam

import (
	"math"
	"slices"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

type option struct {
	candidate agent.ProductCandidate
	class     demand.Classification
	utility   float64
	required  uint16
	optional  uint16
}

func cloneCandidate(c agent.ProductCandidate) agent.ProductCandidate {
	c.Tags = slices.Clone(c.Tags)
	return c
}

func copyState(s demand.State) demand.State {
	s.Required = slices.Clone(s.Required)
	s.Optional = slices.Clone(s.Optional)
	s.Excluded = slices.Clone(s.Excluded)
	s.Preferences = slices.Clone(s.Preferences)
	slices.Sort(s.Required)
	slices.Sort(s.Optional)
	slices.Sort(s.Excluded)
	slices.Sort(s.Preferences)
	return s
}

func categoryBit(categories []demand.Category, category demand.Category) uint16 {
	if i := slices.Index(categories, category); i >= 0 {
		return 1 << i
	}
	return 0
}

// Check size before copying any descriptions. The payload is not trusted
// classification or attribute evidence, even when it fits these bounds.
func validPayload(candidates []agent.ProductCandidate) bool {
	remaining := MaxInputBytes
	for _, c := range candidates {
		if len(c.Tags) > MaxTagsPerSKU {
			return false
		}
		for _, value := range []string{c.Id, c.Name, c.Category, c.Source, c.Evidence.ProductID, c.Evidence.Ranking.Method,
			c.Evidence.DemandCategory.Code, c.Evidence.DemandCategory.TaxonomyVersion} {
			if len(value) > remaining {
				return false
			}
			remaining -= len(value)
		}
		for _, tag := range c.Tags {
			if len(tag) > remaining {
				return false
			}
			remaining -= len(tag)
		}
	}
	return true
}

// Recompute a bounded rank utility; raw similarity and caller-supplied scores
// cannot dominate the objective. Zero evidence deliberately means zero signal.
func rankUtility(r agent.CandidateRanking) (float64, bool) {
	if r == (agent.CandidateRanking{}) {
		return 0, true
	}
	if r.Method != agent.HybridRankingMethod || r.KeywordRank < 0 || r.VectorRank < 0 ||
		r.KeywordRank > agent.MaxCandidateIDs || r.VectorRank > agent.MaxCandidateIDs ||
		r.KeywordRank == 0 && r.VectorRank == 0 || math.IsNaN(r.FusionScore) || math.IsInf(r.FusionScore, 0) {
		return 0, false
	}
	var score float64
	for _, rank := range []int{r.KeywordRank, r.VectorRank} {
		if rank > 0 {
			score += 1 / float64(agent.RankFusionConstant+rank)
		}
	}
	return score, math.Abs(score-r.FusionScore) <= 1e-12
}

// prepare must finish the complete bounded input before exposing any prefix:
// a late duplicate can invalidate an earlier snapshot, even beyond the window.
func (s *Selector) prepare(g *guard, state demand.State, catalog *demand.Catalog, raw []agent.ProductCandidate, stats *Stats) ([]option, error) {
	latest := make(map[string]agent.ProductCandidate, len(raw))
	conflicted := make(map[string]bool)
	for _, c := range raw {
		if stop, err := g.check(); stop || err != nil {
			return nil, err
		}
		if !candidatecontract.ValidID(c.Id) {
			stats.Filtered["invalid_sku"]++
			continue
		}
		if prior, ok := latest[c.Id]; ok {
			stats.DuplicateSnapshots++
			if prior.Evidence.ProductID != c.Evidence.ProductID || prior.Evidence.Source != c.Evidence.Source {
				conflicted[c.Id] = true
			}
		}
		latest[c.Id] = c
	}
	stats.UniqueCandidates = len(latest)
	ids := make([]string, 0, len(latest))
	for id := range latest {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	pool := make([]option, 0, len(ids))
	for _, id := range ids {
		if stop, err := g.check(); stop || err != nil {
			return nil, err
		}
		c := latest[id]
		code := ""
		class := catalog.Classify(c)
		utility, rankingOK := rankUtility(c.Evidence.Ranking)
		switch {
		case conflicted[id] || class.Reason == "identity_mismatch":
			code = "identity_conflict"
		case !candidatecontract.ValidID(c.Evidence.ProductID):
			code = "invalid_parent"
		case c.PriceCents <= 0 || c.PriceCents > agent.MaxBudgetCents || c.Stock <= 0 || c.Sold < 0:
			code = "invalid_facts"
		case !catalog.AcceptsEvidence(c):
			code = "non_demo_evidence"
			if catalog.Scope() != SnapshotScope {
				code = "untrusted_mall_evidence"
			}
		case !rankingOK:
			code = "invalid_ranking"
		case c.PriceCents > state.BudgetCents:
			code = "over_budget"
		case slices.Contains(state.Excluded, class.Category):
			code = "excluded_category"
		case class.Category == demand.Unknown && len(state.Excluded) > 0:
			code = "unknown_with_exclusions"
		}
		if code != "" {
			stats.Filtered[code]++
			continue
		}
		pool = append(pool, option{candidate: cloneCandidate(c), class: class, utility: utility,
			required: categoryBit(state.Required, class.Category), optional: categoryBit(state.Optional, class.Category)})
	}
	stats.EligibleCandidates = len(pool)
	if len(pool) > s.config.MaxCandidates {
		stats.CandidateWindowTruncated = true
		pool = shortlist(pool, state, s.config.MaxCandidates)
	}
	// Ascending IDs give every subset a single enumeration order and make ties
	// independent of the input ordering (except intentional duplicate chronology).
	slices.SortFunc(pool, func(a, b option) int { return compareID(a.candidate.Id, b.candidate.Id) })
	if stop, err := g.check(); stop || err != nil {
		return nil, err
	}
	stats.Prepared = true
	stats.SearchedCandidates = len(pool)
	return pool, nil
}

func shortlist(pool []option, state demand.State, limit int) []option {
	// Reserve one cheapest representative per required category before filling
	// by optional coverage, rank and cost. This mitigates category crowding; it
	// is not a proof that the bounded window contains every feasible bundle.
	chosen := make(map[string]bool, limit)
	out := make([]option, 0, limit)
	for _, category := range state.Required {
		best := -1
		for i, o := range pool {
			if o.class.Category == category && (best < 0 || o.candidate.PriceCents < pool[best].candidate.PriceCents ||
				o.candidate.PriceCents == pool[best].candidate.PriceCents && o.candidate.Id < pool[best].candidate.Id) {
				best = i
			}
		}
		if best >= 0 && len(out) < limit {
			out = append(out, pool[best])
			chosen[pool[best].candidate.Id] = true
		}
	}
	value := slices.Contains(state.Preferences, demand.Preference("value"))
	slices.SortFunc(pool, func(a, b option) int {
		if (a.optional != 0) != (b.optional != 0) {
			if a.optional != 0 {
				return -1
			}
			return 1
		}
		if !value && a.utility != b.utility {
			if a.utility > b.utility {
				return -1
			}
			return 1
		}
		if a.candidate.PriceCents != b.candidate.PriceCents {
			if a.candidate.PriceCents < b.candidate.PriceCents {
				return -1
			}
			return 1
		}
		if a.utility != b.utility {
			if a.utility > b.utility {
				return -1
			}
			return 1
		}
		return compareID(a.candidate.Id, b.candidate.Id)
	})
	for _, o := range pool {
		if len(out) == limit {
			break
		}
		if !chosen[o.candidate.Id] {
			out = append(out, o)
		}
	}
	return out
}

func compareID(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
