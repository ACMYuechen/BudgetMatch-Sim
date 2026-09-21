package beam

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"sync"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	baseline "budgetmatch-sim/services/rpc/agent/internal/recommend"

	"github.com/stretchr/testify/require"
)

func testCatalog(t testing.TB, categories map[string]demand.Category) *demand.Catalog {
	t.Helper()
	ids := make([]string, 0, len(categories))
	for id := range categories {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	entries := make([]demand.CategoryEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, demand.CategoryEntry{SKUID: id, ProductID: "p-" + id, Category: categories[id]})
	}
	// Independently bind this synthetic fixture; it is not a relabeling of the
	// archived retrieval or rule evaluation datasets.
	source, err := json.Marshal(entries)
	require.NoError(t, err)
	binding := demand.DatasetBinding{Version: "beam_test_v1", SHA256: fmt.Sprintf("%x", sha256.Sum256(source))}
	payload, err := json.Marshal(struct {
		demand.CatalogMetadata
		Entries []demand.CategoryEntry `json:"entries"`
	}{demand.CatalogMetadata{Version: "beam_categories_v1", TaxonomyVersion: demand.TaxonomyVersion,
		Provenance: "synthetic_demo", AnnotationStatus: "pending_human_review", DatasetBinding: binding}, entries})
	require.NoError(t, err)
	catalog, err := demand.LoadDemoCatalog(bytes.NewReader(payload), binding)
	require.NoError(t, err)
	return catalog
}

func product(id string, price int64) agent.ProductCandidate {
	return agent.ProductCandidate{Id: id, Name: "UNTRUSTED-NAME", Category: "UNTRUSTED-CATEGORY", Source: "UNTRUSTED-SOURCE",
		PriceCents: price, Stock: 1, Tags: []string{"UNTRUSTED-TAG", "quiet", "battery_life"},
		Evidence: agent.CandidateEvidence{Source: agent.RetrievalDemo, State: agent.VerificationDemo, ProductID: "p-" + id}}
}

func ranked(c agent.ProductCandidate, keyword, vector int) agent.ProductCandidate {
	r := agent.CandidateRanking{Method: agent.HybridRankingMethod, KeywordRank: keyword, VectorRank: vector}
	for _, rank := range []int{keyword, vector} {
		if rank > 0 {
			r.FusionScore += 1 / float64(60+rank)
		}
	}
	c.Evidence.Ranking = r
	return c
}

func testState() demand.State {
	return demand.State{SchemaVersion: demand.SchemaVersion, BudgetCents: 100, MaxItems: 3}
}

func testSelector(t testing.TB, cfg Config) *Selector {
	t.Helper()
	s, err := New(cfg)
	require.NoError(t, err)
	// Keep determinism separate from host load; explicit deadline tests below
	// advance the same cooperative clock without sleeping.
	s.now = func() time.Time { return time.Unix(1, 0) }
	return s
}

func selectedIDs(out Result) []string {
	ids := make([]string, 0, len(out.Selected))
	for _, c := range out.Selected {
		ids = append(ids, c.Id)
	}
	return ids
}

func assertSafe(t testing.TB, state demand.State, catalog *demand.Catalog, out Result) {
	t.Helper()
	require.Equal(t, StrategyVersion, out.Strategy)
	require.Equal(t, SnapshotScope, out.Scope)
	if out.Status == NoFeasibleBundle {
		require.Empty(t, out.Selected)
		require.Nil(t, out.Assessment)
		require.Zero(t, out.TotalPriceCents)
		return
	}
	require.Equal(t, Complete, out.Status)
	require.NotEmpty(t, out.Selected)
	assessment, err := demand.AssessSelection(state, catalog, out.Selected)
	require.NoError(t, err)
	require.True(t, assessment.ConstraintsSatisfied, "%+v", assessment)
	require.Equal(t, assessment.TotalPriceCents, out.TotalPriceCents)
	require.Equal(t, &assessment, out.Assessment)
	for i, c := range out.Selected {
		require.Equal(t, agent.RetrievalDemo, c.Evidence.Source)
		require.Equal(t, agent.VerificationDemo, c.Evidence.State)
		require.Equal(t, catalog.Classify(c), out.Evidence[i].Category)
	}
}

func TestGreedyTrapAndFullCoverage(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Keyboard, "c": demand.Mouse})
	candidates := []agent.ProductCandidate{product("a", 95), product("b", 40), product("c", 40)}
	candidates[0].Sold = 100000000
	state := testState()
	state.Required = []demand.Category{demand.Keyboard, demand.Mouse}
	items, _ := baseline.NewBundleSelector().Select(candidates, agent.Intent{BudgetCents: state.BudgetCents, MaxItems: state.MaxItems})
	require.Len(t, items, 1, "the preserved old baseline does not consume category requirements")
	require.Equal(t, "a", items[0].Id)
	out, err := testSelector(t, Config{}).Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, []string{"b", "c"}, selectedIDs(out))
	assertSafe(t, state, catalog, out)
}

func TestHardConstraintsAndUnknownCategories(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Mouse, "c": demand.Headphones, "u": demand.Unknown})
	candidates := []agent.ProductCandidate{product("a", 40), product("b", 30), product("c", 10), product("u", 1), product("unmapped", 1)}
	tests := []struct {
		name string
		edit func(*demand.State)
		ids  []string
	}{
		{"required_optional_excluded", func(s *demand.State) {
			s.Required = []demand.Category{demand.Keyboard}
			s.Optional = []demand.Category{demand.Mouse}
			s.Excluded = []demand.Category{demand.Headphones}
		}, []string{"a", "b"}},
		{"insufficient_budget", func(s *demand.State) {
			s.Required = []demand.Category{demand.Keyboard, demand.Mouse}
			s.BudgetCents = 60
		}, []string{}},
		{"missing_category", func(s *demand.State) { s.Required = []demand.Category{demand.Monitor} }, []string{}},
		{"unknown_allowed_for_broad_demand", func(s *demand.State) {}, []string{"u"}},
		{"unknown_cannot_prove_exclusion", func(s *demand.State) {
			s.Excluded = []demand.Category{demand.Keyboard, demand.Mouse, demand.Headphones}
		}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testState()
			tt.edit(&state)
			out, err := testSelector(t, Config{}).Select(context.Background(), state, catalog, candidates)
			require.NoError(t, err)
			require.Equal(t, tt.ids, selectedIDs(out))
			assertSafe(t, state, catalog, out)
			if len(state.Excluded) > 0 {
				require.Equal(t, 2, out.Stats.Filtered["unknown_with_exclusions"])
			}
		})
	}
	t.Run("empty_is_not_success", func(t *testing.T) {
		out, err := testSelector(t, Config{}).Select(context.Background(), testState(), catalog, nil)
		require.NoError(t, err)
		require.Equal(t, NoFeasibleBundle, out.Status)
		assertSafe(t, testState(), catalog, out)
	})
}

func TestPreferencesRankingAndFactualExplanation(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Keyboard})
	candidates := []agent.ProductCandidate{ranked(product("a", 70), 1, 1), ranked(product("b", 20), 20, 0)}
	// Raw cosine, sales, names and tags are not ranking or attribute proof.
	candidates[1].Evidence.HasRelevance, candidates[1].Evidence.Relevance = true, math.Inf(1)
	candidates[1].Sold = math.MaxInt64
	state := testState()
	state.MaxItems = 1
	state.Required = []demand.Category{demand.Keyboard}
	state.Preferences = []demand.Preference{"quiet", "battery_life"}
	s := testSelector(t, Config{})
	out, err := s.Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, selectedIDs(out))
	require.Equal(t, []demand.Preference{"battery_life", "quiet"}, out.UnscoredPreferences)
	data, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(data), "UNTRUSTED")
	require.Contains(t, string(data), "mapped_demo")
	state.Preferences = append(state.Preferences, "value")
	cheap, err := s.Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, selectedIDs(cheap))
	// Removing the preference has no persistent side effect in the selector.
	state.Preferences = nil
	removed, err := s.Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, selectedIDs(removed))
	require.Empty(t, removed.UnscoredPreferences)
}

func TestRequiredShortlistReservationAndLatestSnapshots(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Keyboard, "c": demand.Keyboard, "z": demand.Mouse})
	state := testState()
	state.Required = []demand.Category{demand.Mouse, demand.Keyboard}
	candidates := []agent.ProductCandidate{ranked(product("a", 40), 1, 1), ranked(product("b", 30), 2, 2),
		ranked(product("c", 20), 3, 3), product("z", 25)}
	out, err := testSelector(t, Config{MaxCandidates: 2}).Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, []string{"c", "z"}, selectedIDs(out))
	require.True(t, out.Stats.CandidateWindowTruncated)
	require.True(t, out.Stats.SearchLimited)
	assertSafe(t, state, catalog, out)
	narrow, err := testSelector(t, Config{MaxCandidates: 1}).Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, NoFeasibleBundle, narrow.Status)
	require.Equal(t, []demand.Category{demand.Mouse}, narrow.MissingRequiredInWindow)
	// The last snapshot beyond either window revokes the only mouse.
	latest := product("z", 25)
	latest.Stock = 0
	out, err = testSelector(t, Config{MaxCandidates: 2}).Select(context.Background(), state, catalog, append(candidates, latest))
	require.NoError(t, err)
	require.Equal(t, NoFeasibleBundle, out.Status)
	require.Equal(t, 1, out.Stats.DuplicateSnapshots)
	require.Equal(t, 1, out.Stats.Filtered["invalid_facts"])
}

func TestNarrowBeamCanMissFeasibleBundle(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Keyboard, "c": demand.Mouse})
	candidates := []agent.ProductCandidate{ranked(product("a", 90), 1, 1), product("b", 40), product("c", 40)}
	state := testState()
	state.Required = []demand.Category{demand.Keyboard, demand.Mouse}
	narrow, err := testSelector(t, Config{BeamWidth: 1}).Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, NoFeasibleBundle, narrow.Status)
	require.True(t, narrow.Stats.SearchLimited)
	require.Positive(t, narrow.Stats.BeamPruned)
	require.Empty(t, narrow.MissingRequiredInWindow, "all categories exist; search pruning is not global infeasibility")
	wide, err := testSelector(t, Config{BeamWidth: 128}).Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, []string{"b", "c"}, selectedIDs(wide))
	assertSafe(t, state, catalog, wide)
}

func TestDeterminismOwnershipAndConcurrency(t *testing.T) {
	catalog := testCatalog(t, map[string]demand.Category{"a": demand.Keyboard, "b": demand.Mouse, "c": demand.Mouse, "d": demand.Keyboard})
	state := testState()
	state.Required = []demand.Category{demand.Mouse, demand.Keyboard}
	state.Preferences = []demand.Preference{"quiet", "value"}
	candidates := []agent.ProductCandidate{product("d", 25), product("c", 25), product("b", 25), product("a", 25)}
	stateBefore, _ := json.Marshal(state)
	snapshots := make([]agent.ProductCandidate, len(candidates))
	for i, c := range candidates {
		snapshots[i] = cloneCandidate(c)
	}
	s := testSelector(t, Config{})
	first, err := s.Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, selectedIDs(first))
	var group sync.WaitGroup
	for worker := 0; worker < 12; worker++ {
		group.Add(1)
		go func(seed int64) {
			defer group.Done()
			shuffled := slices.Clone(candidates)
			rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			out, err := s.Select(context.Background(), state, catalog, shuffled)
			if err != nil || !slices.Equal(selectedIDs(out), selectedIDs(first)) {
				t.Errorf("non-deterministic selection: %v, %v", selectedIDs(out), err)
			}
		}(int64(worker))
	}
	group.Wait()
	first.Selected[0].Tags[0] = "OUTPUT MUTATION"
	first.Selected[0].PriceCents = 999
	first.Window[0].Tags[0] = "WINDOW MUTATION"
	stateAfter, _ := json.Marshal(state)
	require.Equal(t, stateBefore, stateAfter)
	require.Equal(t, snapshots, candidates)
	replayed, err := s.Select(context.Background(), state, catalog, candidates)
	require.NoError(t, err)
	require.Equal(t, int64(50), replayed.TotalPriceCents)
	require.Equal(t, "UNTRUSTED-TAG", replayed.Selected[0].Tags[0])
	replayed.Window[0].Tags[0] = "SEPARATE WINDOW MUTATION"
	require.Equal(t, "UNTRUSTED-TAG", replayed.Selected[0].Tags[0])
	for _, c := range candidates {
		c.Tags[0] = "INPUT MUTATION"
	}
	require.Equal(t, "UNTRUSTED-TAG", replayed.Selected[0].Tags[0])
}
