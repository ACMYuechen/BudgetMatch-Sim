package beam

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"

	"github.com/stretchr/testify/require"
)

func TestConfigAndInputBounds(t *testing.T) {
	for _, cfg := range []Config{{MaxCandidates: -1}, {MaxCandidates: 257}, {BeamWidth: -1}, {BeamWidth: 129},
		{MaxExpansions: -1}, {MaxExpansions: MaxExpansions + 1}, {TimeBudget: -1}, {TimeBudget: MaxTimeBudget + 1}} {
		_, err := New(cfg)
		require.ErrorIs(t, err, ErrConfig)
	}
	s := testSelector(t, Config{})
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard})
	state := testState()
	tests := []struct {
		name string
		edit func(*demand.State, **demand.Catalog, *[]agent.ProductCandidate)
	}{
		{"bad_state", func(s *demand.State, _ **demand.Catalog, _ *[]agent.ProductCandidate) { s.BudgetCents = 0 }},
		{"conflict", func(s *demand.State, _ **demand.Catalog, _ *[]agent.ProductCandidate) {
			s.Required, s.Excluded = []demand.Category{demand.Keyboard}, []demand.Category{demand.Keyboard}
		}},
		{"nil_catalog", func(_ *demand.State, c **demand.Catalog, _ *[]agent.ProductCandidate) { *c = nil }},
		{"zero_catalog", func(_ *demand.State, c **demand.Catalog, _ *[]agent.ProductCandidate) { *c = &demand.Catalog{} }},
		{"raw_count", func(_ *demand.State, _ **demand.Catalog, c *[]agent.ProductCandidate) {
			*c = make([]agent.ProductCandidate, 257)
		}},
		{"payload", func(_ *demand.State, _ **demand.Catalog, c *[]agent.ProductCandidate) {
			(*c)[0].Name = strings.Repeat("x", MaxInputBytes+1)
		}},
		{"aggregate_payload", func(_ *demand.State, _ **demand.Catalog, c *[]agent.ProductCandidate) {
			(*c)[0].Name = strings.Repeat("x", MaxInputBytes/2)
			*c = append(*c, (*c)[0])
		}},
		{"tags", func(_ *demand.State, _ **demand.Catalog, c *[]agent.ProductCandidate) {
			(*c)[0].Tags = make([]string, MaxTagsPerSKU+1)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, cat, candidates := state, catalog, []agent.ProductCandidate{product("a", 10)}
			tt.edit(&snapshot, &cat, &candidates)
			out, err := s.Select(context.Background(), snapshot, cat, candidates)
			require.ErrorIs(t, err, ErrInput)
			require.Equal(t, Result{}, out)
		})
	}
	_, err := (&Selector{}).Select(context.Background(), state, catalog, nil)
	require.ErrorIs(t, err, ErrConfig)
	_, err = s.Select(nil, state, catalog, nil)
	require.ErrorIs(t, err, ErrInput)
	duplicates := make([]agent.ProductCandidate, agent.MaxCandidateIDs)
	for i := range duplicates {
		duplicates[i] = product("a", 10)
	}
	out, err := s.Select(context.Background(), state, catalog, duplicates)
	require.NoError(t, err)
	require.Equal(t, 255, out.Stats.DuplicateSnapshots)
	require.Equal(t, []string{"a"}, selectedIDs(out))
}

func TestUnsafeLatestSnapshotsCannotResurrect(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard})
	tests := []struct {
		name string
		edit func(*agent.ProductCandidate)
		code string
	}{
		{"zero_price", func(c *agent.ProductCandidate) { c.PriceCents = 0 }, "invalid_facts"},
		{"negative_price", func(c *agent.ProductCandidate) { c.PriceCents = -1 }, "invalid_facts"},
		{"overflow_price", func(c *agent.ProductCandidate) { c.PriceCents = math.MaxInt64 }, "invalid_facts"},
		{"stock", func(c *agent.ProductCandidate) { c.Stock = 0 }, "invalid_facts"},
		{"sold", func(c *agent.ProductCandidate) { c.Sold = -1 }, "invalid_facts"},
		{"parent_conflict", func(c *agent.ProductCandidate) { c.Evidence.ProductID = "different" }, "identity_conflict"},
		{"source_conflict", func(c *agent.ProductCandidate) { c.Evidence.Source = agent.RetrievalMallKeyword }, "identity_conflict"},
		{"unverified", func(c *agent.ProductCandidate) { c.Evidence.State = agent.VerificationUnverified }, "non_demo_evidence"},
		{"nan_rank", func(c *agent.ProductCandidate) { c.Evidence.Ranking.FusionScore = math.NaN() }, "invalid_ranking"},
		{"infinite_rank", func(c *agent.ProductCandidate) { c.Evidence.Ranking.FusionScore = math.Inf(1) }, "invalid_ranking"},
		{"inconsistent_rank", func(c *agent.ProductCandidate) { *c = ranked(*c, 1, 0); c.Evidence.Ranking.FusionScore = 0.9 }, "invalid_ranking"},
		{"negative_rank", func(c *agent.ProductCandidate) { *c = ranked(*c, -1, 0) }, "invalid_ranking"},
		{"rank_bound", func(c *agent.ProductCandidate) { *c = ranked(*c, 257, 0) }, "invalid_ranking"},
		{"empty_lanes", func(c *agent.ProductCandidate) { *c = ranked(*c, 0, 0) }, "invalid_ranking"},
		{"unknown_method", func(c *agent.ProductCandidate) { c.Evidence.Ranking.Method = "model_score" }, "invalid_ranking"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := product("a", 10)
			tt.edit(&bad)
			out, err := testSelector(t, Config{}).Select(context.Background(), testState(), catalog, []agent.ProductCandidate{product("a", 10), bad})
			require.NoError(t, err)
			require.Equal(t, NoFeasibleBundle, out.Status)
			require.Equal(t, 1, out.Stats.Filtered[tt.code])
			if tt.code == "identity_conflict" {
				out, err = testSelector(t, Config{}).Select(context.Background(), testState(), catalog, []agent.ProductCandidate{bad, product("a", 10)})
				require.NoError(t, err)
				require.Equal(t, NoFeasibleBundle, out.Status, "identity conflicts cannot be fixed by ordering")
			}
		})
	}
	for _, id := range []string{"", " a", "a ", "a\x00", string([]byte{0xff}), strings.Repeat("a", 65)} {
		out, err := testSelector(t, Config{}).Select(context.Background(), testState(), catalog, []agent.ProductCandidate{product(id, 10)})
		require.NoError(t, err)
		require.Equal(t, NoFeasibleBundle, out.Status)
		require.Equal(t, 1, out.Stats.Filtered["invalid_sku"])
	}
	t.Run("real_candidate_cannot_borrow_demo_mapping", func(t *testing.T) {
		c := product("a", 10)
		c.Evidence.Source, c.Evidence.State = agent.RetrievalMallKeyword, agent.VerificationChecked
		out, err := testSelector(t, Config{}).Select(context.Background(), testState(), catalog, []agent.ProductCandidate{c})
		require.NoError(t, err)
		require.Equal(t, 1, out.Stats.Filtered["non_demo_evidence"])
		require.Equal(t, NoFeasibleBundle, out.Status)
	})
}

func TestExpansionAndTimeBounds(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Mouse})
	candidates := []agent.ProductCandidate{product("a", 20), product("b", 20)}
	state := testState()
	state.Required = []demand.Category{demand.Keyboard, demand.Mouse}
	for limit := 1; limit <= 3; limit++ {
		out, err := testSelector(t, Config{MaxExpansions: limit}).Select(context.Background(), state, catalog, candidates)
		require.NoError(t, err)
		require.Equal(t, limit, out.Stats.Expansions)
		if limit < 3 {
			require.Equal(t, NoFeasibleBundle, out.Status)
			require.Equal(t, stopExpansions, out.Stats.StopReason)
			require.True(t, out.Stats.SearchLimited)
		} else {
			require.Equal(t, Complete, out.Status)
			require.False(t, out.Stats.SearchLimited, "exactly finishing at a cap is not truncation")
		}
		assertSafe(t, state, catalog, out)
	}
	out, err := testSelector(t, Config{MaxExpansions: 1}).Select(context.Background(), testState(), catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, Complete, out.Status, "an already feasible incumbent survives the internal cap")
	require.True(t, out.Stats.SearchLimited)
	for _, tc := range []struct {
		name   string
		expire int
		found  bool
	}{
		{"before_preparation", 2, false},
		{"during_normalization", 3, false},
		{"during_classification", 5, false},
		{"before_search", 7, false},
		{"after_incumbent", 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSelector(t, Config{TimeBudget: time.Millisecond})
			calls := 0
			s.now = func() time.Time {
				calls++
				if calls >= tc.expire {
					return time.Unix(1, 0).Add(time.Millisecond)
				}
				return time.Unix(1, 0)
			}
			out, err := s.Select(context.Background(), testState(), catalog, candidates)
			require.NoError(t, err)
			require.Equal(t, tc.found, out.Status == Complete)
			require.True(t, out.Stats.TimeBudgetReached)
			require.Equal(t, stopTime, out.Stats.StopReason)
			assertSafe(t, testState(), catalog, out)
		})
	}
}

func TestMaximumPriceAndItemBounds(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Mouse})
	state := testState()
	state.BudgetCents, state.MaxItems = agent.MaxBudgetCents, agent.MaxItems
	state.Required = []demand.Category{demand.Keyboard, demand.Mouse}
	candidates := []agent.ProductCandidate{product("a", agent.MaxBudgetCents-1), product("b", 1)}
	out, err := testSelector(t, Config{}).Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, agent.MaxBudgetCents, out.TotalPriceCents)
	assertSafe(t, state, catalog, out)
	candidates[0].PriceCents++
	out, err = testSelector(t, Config{}).Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, NoFeasibleBundle, out.Status)
	// Enough budget must not bypass the item count or duplicate-SKU rules.
	state.Required = nil
	state.MaxItems = 1
	candidates[0] = ranked(product("a", 1), 1, 0)
	candidates[1] = ranked(product("b", 1), 1, 0)
	out, err = testSelector(t, Config{}).Select(context.Background(), state, catalog, append(candidates, candidates[0]))
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, selectedIDs(out))
	assertSafe(t, state, catalog, out)
}

func TestCancellationDiscardsEvenFeasibleIncumbent(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Mouse})
	for _, at := range []int{0, 2, 5, 7, 8, 9, 10} {
		t.Run(strconv.Itoa(at), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if at == 0 {
				cancel()
			}
			s := testSelector(t, Config{})
			calls := 0
			s.now = func() time.Time {
				calls++
				if calls == at {
					cancel()
				}
				return time.Unix(1, 0)
			}
			out, err := s.Select(ctx, testState(), catalog, []agent.ProductCandidate{product("a", 20), product("b", 20)})
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, Result{}, out)
		})
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	out, err := testSelector(t, Config{}).Select(ctx, testState(), catalog, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, Result{}, out)
}
