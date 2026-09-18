package beam

import (
	"context"
	"math/bits"
	"slices"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
)

type Stats struct {
	InputSnapshots           int            `json:"input_snapshots"`
	UniqueCandidates         int            `json:"unique_candidates"`
	DuplicateSnapshots       int            `json:"duplicate_snapshots"`
	EligibleCandidates       int            `json:"eligible_candidates"`
	SearchedCandidates       int            `json:"searched_candidates"`
	Filtered                 map[string]int `json:"filtered"`
	Prepared                 bool           `json:"prepared"`
	CandidateWindowTruncated bool           `json:"candidate_window_truncated"`
	Expansions               int            `json:"expansions"`
	FeasibleEvaluated        int            `json:"feasible_evaluated"`
	PeakFrontier             int            `json:"peak_frontier"`
	BeamPruned               int            `json:"beam_pruned"`
	TimeBudgetReached        bool           `json:"time_budget_reached"`
	StopReason               string         `json:"stop_reason"`
	SearchLimited            bool           `json:"search_limited"`
}

type ItemEvidence struct {
	SKUID       string                `json:"sku_id"`
	PriceCents  int64                 `json:"price_cents"`
	Category    demand.Classification `json:"category"`
	Required    bool                  `json:"required"`
	Optional    bool                  `json:"optional"`
	RankUtility float64               `json:"rank_utility"`
}

type Result struct {
	Strategy string                   `json:"strategy"`
	Scope    string                   `json:"scope"`
	Status   string                   `json:"status"`
	Selected []agent.ProductCandidate `json:"-"`
	// Window is the complete prepared search scope, not the raw input prefix.
	// A downstream snapshot check must not add candidates outside this scope.
	Window                  []agent.ProductCandidate `json:"-"`
	TotalPriceCents         int64                    `json:"total_price_cents"`
	Assessment              *demand.Assessment       `json:"assessment,omitempty"`
	Evidence                []ItemEvidence           `json:"evidence"`
	MissingRequiredInWindow []demand.Category        `json:"missing_required_in_window,omitempty"`
	UnscoredPreferences     []demand.Preference      `json:"unscored_preferences"`
	Stats                   Stats                    `json:"stats"`
}

type guard struct {
	ctx      context.Context
	now      func() time.Time
	deadline time.Time
	expired  bool
}

func (g *guard) check() (bool, error) {
	if err := g.ctx.Err(); err != nil {
		return false, err
	}
	if !g.now().Before(g.deadline) {
		g.expired = true
	}
	// A test clock may deliver cancellation while advancing. Also protects the
	// boundary before returning an incumbent after internal timeout.
	if err := g.ctx.Err(); err != nil {
		return false, err
	}
	return g.expired, nil
}

type node struct {
	indices  [agent.MaxItems]int
	count    int
	price    int64
	utility  float64
	required uint16
	optional uint16
}

// Select returns only full hard-constraint solutions, never an actionable
// partial bundle. NoFeasibleBundle means not found in this bounded demo search,
// NOT global infeasibility. External cancellation returns an error and no result.
func (s *Selector) Select(ctx context.Context, state demand.State, catalog *demand.Catalog, candidates []agent.ProductCandidate) (Result, error) {
	if s == nil || !s.config.valid() || s.now == nil {
		return Result{}, ErrConfig
	}
	if ctx == nil {
		return Result{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	g := guard{ctx: ctx, now: s.now, deadline: s.now().Add(s.config.TimeBudget)}
	_, digest := catalog.Metadata()
	if state.Validate() != nil || digest == "" || len(candidates) > agent.MaxCandidateIDs || !validPayload(candidates) {
		return Result{}, ErrInput
	}
	state = copyState(state)
	out := Result{Strategy: StrategyVersion, Scope: SnapshotScope, Status: NoFeasibleBundle,
		Evidence: []ItemEvidence{}, UnscoredPreferences: []demand.Preference{},
		Stats: Stats{InputSnapshots: len(candidates), Filtered: map[string]int{}, StopReason: stopFinished}}
	for _, p := range state.Preferences {
		if p != "value" {
			out.UnscoredPreferences = append(out.UnscoredPreferences, p)
		}
	}
	pool, err := s.prepare(&g, state, catalog, candidates, &out.Stats)
	if err != nil {
		return Result{}, err
	}
	var best node
	if out.Stats.Prepared {
		var available uint16
		for _, o := range pool {
			out.Window = append(out.Window, cloneCandidate(o.candidate))
			available |= o.required
		}
		for i, category := range state.Required {
			if available&(1<<i) == 0 {
				out.MissingRequiredInWindow = append(out.MissingRequiredInWindow, category)
			}
		}
		// Missing category evidence already rules out a full solution in this
		// window, but says nothing about the unsearched catalog outside it.
		if len(out.MissingRequiredInWindow) == 0 {
			best, err = s.search(&g, state, pool, &out.Stats)
			if err != nil {
				return Result{}, err
			}
		}
	}
	if best.count > 0 {
		for _, i := range best.indices[:best.count] {
			o := pool[i]
			out.Selected = append(out.Selected, cloneCandidate(o.candidate))
			out.Evidence = append(out.Evidence, ItemEvidence{SKUID: o.candidate.Id, PriceCents: o.candidate.PriceCents,
				Category: o.class, Required: o.required != 0, Optional: o.optional != 0, RankUtility: o.utility})
		}
		// Independent domain revalidation is mandatory even for an incumbent
		// found before a limit. Budget arithmetic never substitutes for this.
		assessment, err := demand.AssessSelection(state, catalog, out.Selected)
		if err != nil || !assessment.ConstraintsSatisfied || assessment.TotalPriceCents != best.price {
			return Result{}, agent.ErrUnsafeResult
		}
		out.Status, out.TotalPriceCents, out.Assessment = Complete, best.price, &assessment
	}
	if _, err := g.check(); err != nil {
		return Result{}, err
	}
	out.Stats.TimeBudgetReached = g.expired
	if g.expired {
		out.Stats.StopReason = stopTime
	}
	out.Stats.SearchLimited = g.expired || out.Stats.StopReason == stopExpansions ||
		out.Stats.CandidateWindowTruncated || out.Stats.BeamPruned > 0
	return out, nil
}

func (s *Selector) search(g *guard, state demand.State, pool []option, stats *Stats) (node, error) {
	frontier := []node{{}}
	var best node
	allRequired := uint16(1<<len(state.Required)) - 1
	value := slices.Contains(state.Preferences, demand.Preference("value"))
	less := func(a, b node) int { return compareNode(a, b, pool, value) }
	for depth := 1; depth <= int(state.MaxItems) && len(frontier) > 0; depth++ {
		stats.PeakFrontier = max(stats.PeakFrontier, len(frontier))
		var next []node
		if depth < int(state.MaxItems) {
			next = make([]node, 0, min(s.config.BeamWidth*len(pool), s.config.MaxExpansions-stats.Expansions))
		}
		for _, parent := range frontier {
			start := 0
			if parent.count > 0 {
				start = parent.indices[parent.count-1] + 1
			}
			for i := start; i < len(pool); i++ {
				if stop, err := g.check(); stop || err != nil {
					return best, err
				}
				if stats.Expansions == s.config.MaxExpansions {
					stats.StopReason = stopExpansions
					return best, nil
				}
				stats.Expansions++ // Includes over-budget and dominated attempts.
				o := pool[i]
				if o.candidate.PriceCents > state.BudgetCents-parent.price {
					continue
				}
				child := parent
				child.indices[parent.count], child.count = i, parent.count+1
				child.price += o.candidate.PriceCents
				child.utility += o.utility
				child.required |= o.required
				child.optional |= o.optional
				// Positive-cost, zero-signal filler cannot improve any objective.
				// A first item is allowed for broad demand; empty never succeeds.
				if parent.count > 0 && child.required == parent.required && child.optional == parent.optional && o.utility == 0 {
					continue
				}
				if child.required == allRequired {
					stats.FeasibleEvaluated++
					if best.count == 0 || less(child, best) < 0 {
						best = child
					}
				}
				if depth < int(state.MaxItems) && i+1 < len(pool) {
					next = append(next, child)
				}
			}
		}
		if len(next) > s.config.BeamWidth {
			slices.SortFunc(next, less)
			stats.BeamPruned += len(next) - s.config.BeamWidth
			next = next[:s.config.BeamWidth]
		}
		frontier = next
	}
	return best, nil
}

// Lexicographic, versioned objective: hard coverage > optional coverage >
// rank utility > price > item count > SKU bytes. "value" moves price before
// rank, meaning lower cost at equal coverage, not an unsupported quality claim.
func compareNode(a, b node, pool []option, value bool) int {
	for _, pair := range [][2]int{{bits.OnesCount16(a.required), bits.OnesCount16(b.required)},
		{bits.OnesCount16(a.optional), bits.OnesCount16(b.optional)}} {
		if pair[0] != pair[1] {
			if pair[0] > pair[1] {
				return -1
			}
			return 1
		}
	}
	if !value && a.utility != b.utility {
		if a.utility > b.utility {
			return -1
		}
		return 1
	}
	if a.price != b.price {
		if a.price < b.price {
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
	if a.count != b.count {
		if a.count < b.count {
			return -1
		}
		return 1
	}
	for i := 0; i < a.count; i++ {
		if order := compareID(pool[a.indices[i]].candidate.Id, pool[b.indices[i]].candidate.Id); order != 0 {
			return order
		}
	}
	return 0
}
