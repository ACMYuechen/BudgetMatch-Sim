package beam

import (
	"context"
	"fmt"
	"math/bits"
	"math/rand"
	"slices"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"

	"github.com/stretchr/testify/require"
)

type oracleBundle struct {
	ids      []string
	optional int
	price    int64
	rank     float64
}

// Exhaustive, independently written oracle for <= 8 unique, valid fixture
// candidates. It does not call beam preparation, comparison or AssessSelection.
// Category labels come directly from the synthetic fixture, not candidate text.
func exhaustive(state demand.State, categories map[string]demand.Category, candidates []agent.ProductCandidate) []string {
	candidates = slices.Clone(candidates)
	slices.SortFunc(candidates, func(a, b agent.ProductCandidate) int {
		if a.Id < b.Id {
			return -1
		}
		if a.Id > b.Id {
			return 1
		}
		return 0
	})
	var best *oracleBundle
	for mask := 1; mask < 1<<len(candidates); mask++ {
		if bits.OnesCount(uint(mask)) > int(state.MaxItems) {
			continue
		}
		bundle := oracleBundle{}
		covered := map[demand.Category]bool{}
		valid := true
		for i, c := range candidates {
			if mask&(1<<i) == 0 {
				continue
			}
			category := categories[c.Id]
			if c.PriceCents <= 0 || c.Stock <= 0 || slices.Contains(state.Excluded, category) ||
				category == demand.Unknown && len(state.Excluded) > 0 {
				valid = false
				break
			}
			covered[category] = true
			bundle.ids = append(bundle.ids, c.Id)
			bundle.price += c.PriceCents
			// Independently recompute the documented RRF objective per SKU.
			var utility float64
			if c.Evidence.Ranking.KeywordRank > 0 {
				utility += 1 / float64(60+c.Evidence.Ranking.KeywordRank)
			}
			if c.Evidence.Ranking.VectorRank > 0 {
				utility += 1 / float64(60+c.Evidence.Ranking.VectorRank)
			}
			bundle.rank += utility
		}
		for _, required := range state.Required {
			valid = valid && covered[required]
		}
		if !valid || bundle.price > state.BudgetCents {
			continue
		}
		for _, optional := range state.Optional {
			if covered[optional] {
				bundle.optional++
			}
		}
		if best == nil || oracleBetter(bundle, *best, slices.Contains(state.Preferences, demand.Preference("value"))) {
			best = &bundle
		}
	}
	if best == nil {
		return []string{}
	}
	return best.ids
}

func oracleBetter(a, b oracleBundle, value bool) bool {
	if a.optional != b.optional {
		return a.optional > b.optional
	}
	if value && a.price != b.price {
		return a.price < b.price
	}
	if a.rank != b.rank {
		return a.rank > b.rank
	}
	if a.price != b.price {
		return a.price < b.price
	}
	if len(a.ids) != len(b.ids) {
		return len(a.ids) < len(b.ids)
	}
	return slices.Compare(a.ids, b.ids) < 0
}

func TestWideBeamAgainstIndependentExhaustiveOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(4201))
	classes := []demand.Category{demand.Keyboard, demand.Mouse, demand.Lighting, demand.Headphones, demand.Unknown}
	s := testSelector(t, Config{MaxCandidates: 8, BeamWidth: 128, MaxExpansions: 65536})
	for trial := 0; trial < 256; trial++ {
		t.Run(fmt.Sprintf("fixture_%03d", trial), func(t *testing.T) {
			n := 1 + rng.Intn(8)
			categories := map[string]demand.Category{}
			candidates := make([]agent.ProductCandidate, n)
			for i := range candidates {
				id := fmt.Sprintf("s%02d", i)
				categories[id] = classes[rng.Intn(len(classes))]
				candidates[i] = product(id, int64(1+rng.Intn(70)))
				if rng.Intn(3) != 0 {
					candidates[i] = ranked(candidates[i], 1+rng.Intn(50), rng.Intn(50))
				}
			}
			state := testState()
			state.BudgetCents = int64(1 + rng.Intn(180))
			state.MaxItems = int32(2 + rng.Intn(4))
			if trial%3 > 0 {
				state.Required = []demand.Category{demand.Keyboard}
			}
			if trial%3 == 2 {
				state.Required = append(state.Required, demand.Mouse)
			}
			if trial%4 != 0 {
				state.Optional = []demand.Category{demand.Lighting}
			}
			if trial%5 != 0 {
				state.Excluded = []demand.Category{demand.Headphones}
			}
			if trial%2 == 0 {
				state.Preferences = []demand.Preference{"value", "quiet"}
			}
			catalog := testCatalog(t, categories)
			expected := exhaustive(state, categories, candidates)
			out, err := s.Select(context.Background(), state, catalog, candidates)
			require.NoError(t, err)
			require.Equal(t, expected, selectedIDs(out))
			require.False(t, out.Stats.SearchLimited)
			require.LessOrEqual(t, out.Stats.Expansions, 255)
			assertSafe(t, state, catalog, out)
		})
	}
}

func FuzzBoundedSelectionSafety(f *testing.F) {
	f.Add([]byte{8, 4, 2, 90, 0, 10, 5, 20})
	f.Add([]byte{1})
	f.Add([]byte{255, 0, 255, 0, 255})
	categories := map[string]demand.Category{}
	for i := 0; i < 8; i++ {
		categories[fmt.Sprintf("s%d", i)] = []demand.Category{demand.Keyboard, demand.Mouse, demand.Lighting, demand.Unknown}[i%4]
	}
	catalog := testCatalog(f, categories)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		at := func(i int) byte { return data[i%len(data)] }
		state := testState()
		state.BudgetCents = 1 + int64(at(1))
		state.Required = []demand.Category{demand.Keyboard, demand.Mouse}
		state.Optional = []demand.Category{demand.Lighting}
		if at(2)%2 == 0 {
			state.Excluded = []demand.Category{demand.Headphones}
		}
		if at(3)%2 == 0 {
			state.Preferences = []demand.Preference{"value"}
		}
		var candidates []agent.ProductCandidate
		for i := 0; i < 1+int(at(0))%8; i++ {
			c := product(fmt.Sprintf("s%d", int(at(i+4))%8), int64(at(i+5)))
			c.Stock = int64(at(i+6) % 3)
			switch at(i+7) % 6 {
			case 0:
				c = ranked(c, 1+int(at(i+8)), 0)
			case 1:
				c.Evidence.ProductID = "conflict"
			case 2:
				c.Evidence.Source = agent.RetrievalMallVector
			case 3:
				c.Id += " "
			}
			candidates = append(candidates, c)
		}
		s := testSelector(t, Config{MaxCandidates: 1 + int(at(0))%8, BeamWidth: 1 + int(at(1))%8, MaxExpansions: 1 + int(at(2))%64})
		out, err := s.Select(context.Background(), state, catalog, candidates)
		require.NoError(t, err)
		assertSafe(t, state, catalog, out)
		require.LessOrEqual(t, out.Stats.Expansions, s.config.MaxExpansions)
		require.LessOrEqual(t, out.Stats.SearchedCandidates, s.config.MaxCandidates)
		require.LessOrEqual(t, out.Stats.PeakFrontier, s.config.BeamWidth)
		latest := map[string]agent.ProductCandidate{}
		for _, c := range candidates {
			latest[c.Id] = c
		}
		for _, c := range out.Selected {
			require.Equal(t, latest[c.Id], c)
		}
		repeat, err := s.Select(context.Background(), state, catalog, candidates)
		require.NoError(t, err)
		require.Equal(t, out, repeat)
	})
}

func BenchmarkSelect(b *testing.B) {
	for _, size := range []int{8, 64, 256} {
		b.Run(fmt.Sprintf("candidates_%d", size), func(b *testing.B) {
			categories := map[string]demand.Category{}
			candidates := make([]agent.ProductCandidate, size)
			for i := range candidates {
				id := fmt.Sprintf("s%03d", i)
				categories[id] = []demand.Category{demand.Keyboard, demand.Mouse, demand.Lighting}[i%3]
				candidates[i] = ranked(product(id, int64(100+i*7)), i+1, size-i)
			}
			catalog := testCatalog(b, categories)
			state := testState()
			state.BudgetCents, state.MaxItems = 1800, 5
			state.Required, state.Optional = []demand.Category{demand.Keyboard, demand.Mouse}, []demand.Category{demand.Lighting}
			s, err := New(Config{TimeBudget: time.Second})
			require.NoError(b, err)
			b.ReportAllocs()
			b.ResetTimer()
			var out Result
			for i := 0; i < b.N; i++ {
				out, err = s.Select(context.Background(), state, catalog, candidates)
				if err != nil || out.Status != Complete {
					b.Fatalf("unexpected result: %s, %v", out.Status, err)
				}
			}
			b.ReportMetric(float64(out.Stats.Expansions), "expansions/op")
			b.ReportMetric(float64(out.Stats.SearchedCandidates), "window/op")
		})
	}
}
