package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/recommend/beam"
	"github.com/stretchr/testify/require"
)

func loadDemandTest(t testing.TB) DemandDataset {
	t.Helper()
	data, err := os.ReadFile("../../testdata/eval/demand.v1.json")
	require.NoError(t, err)
	d, err := LoadDemand(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), d.sha256)
	return d
}

func TestDemandReplayRetainsCounterexamplesAndSeparateDenominators(t *testing.T) {
	d := loadDemandTest(t)
	r, err := RunDemand(context.Background(), d, "test-worktree")
	require.NoError(t, err)
	require.True(t, r.GatePassed, "%+v", r.Execution)
	require.Len(t, r.Selection, 5)
	require.Len(t, r.Execution, 18)
	require.Len(t, r.Metadata.ProtocolSHA256, 64)
	require.Equal(t, "not_run", r.RealMall)
	require.Equal(t, 0, r.ModelCalls)
	require.Nil(t, r.TokenUsage)
	require.Nil(t, r.CostUSD)
	for _, s := range r.Selection {
		require.Len(t, s.Cases, 17)
	}
	legacy, standard, narrow := r.Selection[0], r.Selection[1], r.Selection[2]
	require.Nil(t, legacy.Summary.SearchExpansions)
	require.Equal(t, ratio(11, 11), standard.Summary.SatisfiableSuccess)
	require.Equal(t, ratio(0, 11), standard.Summary.HardViolationRate)
	require.Equal(t, ratio(11, 11), standard.Summary.OracleOptimumMatch)
	require.Less(t, legacy.Summary.SatisfiableSuccess.Numerator, standard.Summary.SatisfiableSuccess.Numerator)
	require.Positive(t, legacy.Summary.HardViolationRate.Numerator)
	require.Equal(t, "invalid_bundle", legacy.Cases[0].MissReason)
	require.Equal(t, "beam_pruning", narrow.Cases[1].MissReason)
	require.True(t, narrow.Cases[1].Oracle.Feasible)
	require.Equal(t, "candidate_window", r.Selection[3].Cases[0].MissReason)
	require.Equal(t, "expansion_limit", r.Selection[4].Cases[0].MissReason)
	require.Equal(t, ratio(18, 18), r.ExecutionSummary.OutcomeAccuracy)
	require.Equal(t, ratio(9, 9), r.ExecutionSummary.FaultClosed)
	require.Equal(t, ratio(6, 7), r.ExecutionSummary.SnapshotSuccess, "window loss must stay in the feasible denominator")
	require.Equal(t, ratio(0, 6), r.ExecutionSummary.HardViolationRate)
	require.Equal(t, 29, r.ExecutionSummary.CheckCalls)
	for _, c := range r.Execution {
		require.Empty(t, c.GateProblems, c.ID)
		if c.ID == "outside-window-not-revived" {
			require.True(t, c.FinalOracle.Feasible)
			require.False(t, c.TaskSuccess)
			require.Equal(t, []string{"mouse"}, c.CheckIDs[1])
		}
		if slices.Contains([]string{"repriced", "off-shelf", "category-changed", "category-withdrawn"}, c.ID) {
			require.Equal(t, []string{"alt-key", "mouse"}, c.SelectedIDs, c.ID)
			require.EqualValues(t, 85, c.TotalCents)
		}
	}
	md := DemandMarkdown(r)
	for _, text := range []string{"不是线上 A/B", "生产 Demand 拒绝保护不变", "outside-window-not-revived", "未插桩", "Token/费用 null", "窄 Beam"} {
		require.Contains(t, md, text)
	}
	// Guard remains installed; the baseline is a numeric algorithm projection.
	_, err = agent.NewConstraints(demandIntent(d.fixture.Selection[0].State))
	require.ErrorIs(t, err, agent.ErrDemandNotExecutable)
}

func scrubDemandTiming(r *DemandReport) {
	for i := range r.Selection {
		r.Selection[i].Summary.ReplayP50MS, r.Selection[i].Summary.ReplayP95MS = 0, 0
		for j := range r.Selection[i].Cases {
			r.Selection[i].Cases[j].ReplayMS = 0
		}
	}
	for i := range r.Execution {
		r.Execution[i].ReplayMS = 0
	}
	r.ExecutionSummary.ReplayP50MS, r.ExecutionSummary.ReplayP95MS = 0, 0
}

func TestDemandReplayDeterminismOwnershipAndCancellation(t *testing.T) {
	d := loadDemandTest(t)
	before, err := json.Marshal(d.fixture)
	require.NoError(t, err)
	a, err := RunDemand(context.Background(), d, "fixed")
	require.NoError(t, err)
	b, err := RunDemand(context.Background(), d, "fixed")
	require.NoError(t, err)
	scrubDemandTiming(&a)
	scrubDemandTiming(&b)
	require.Equal(t, a, b)
	// Output slices must not alias the private data or another report.
	a.Selection[1].Cases[0].SelectedIDs[0] = "mutated"
	a.Execution[0].CheckIDs[0][0] = "mutated"
	after, err := json.Marshal(d.fixture)
	require.NoError(t, err)
	require.Equal(t, before, after)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = RunDemand(ctx, d, "canceled")
	require.ErrorIs(t, err, context.Canceled)
	_, err = RunDemand(context.Background(), DemandDataset{}, "zero")
	require.Error(t, err)
}

func TestDemandExpectationsCannotDriveInjectedResultsOrHideFaults(t *testing.T) {
	d := loadDemandTest(t)
	d.fixture.Execution[0].Expected = "no_feasible_bundle"
	d.fixture.Execution[1].ExpectedCalls = 1
	r, err := RunDemand(context.Background(), d, "bad-label")
	require.NoError(t, err)
	require.False(t, r.GatePassed)
	require.Equal(t, "complete", r.Execution[0].Outcome)
	require.False(t, r.Execution[0].OutcomeMatched)
	require.Equal(t, 2, r.Execution[1].CheckCalls)
	require.Contains(t, r.Execution[1].GateProblems, "call_bounds_or_expectation")
}

func TestDemandLoaderRejectsAmbiguousUnboundedOrInvalidFixtures(t *testing.T) {
	valid := loadDemandTest(t)
	data, err := json.Marshal(valid.fixture)
	require.NoError(t, err)
	for _, bad := range [][]byte{
		[]byte(`{"version":"x","VERSION":"y"}`), []byte(`{"version":"x","ver\u0073ion":"y"}`),
		append(slices.Clone(data), []byte(` {}`)...), append([]byte{0xff}, data...),
		[]byte(strings.Repeat(" ", demandFixtureLimit+1)),
		[]byte(strings.Repeat("[", 18) + strings.Repeat("]", 18)),
		bytes.Replace(data, []byte(`"version":`), []byte(`"extra":1,"version":`), 1),
	} {
		_, err := LoadDemand(bytes.NewReader(bad))
		require.Error(t, err)
	}
	for name, mutate := range map[string]func(*DemandFixture){
		"real provenance":   func(f *DemandFixture) { f.Provenance = "real" },
		"review claim":      func(f *DemandFixture) { f.AnnotationStatus = "reviewed" },
		"duplicate product": func(f *DemandFixture) { f.Products = append(f.Products, f.Products[0]) },
		"invalid category":  func(f *DemandFixture) { f.Products[0].Category = "invented" },
		"missing revision":  func(f *DemandFixture) { f.Products[0].Revision = 0 },
		"price overflow":    func(f *DemandFixture) { f.Products[0].PriceCents = agent.MaxBudgetCents + 1 },
		"rank too large":    func(f *DemandFixture) { f.Products[0].KeywordRank = 257 },
		"invalid state":     func(f *DemandFixture) { f.Selection[0].State.BudgetCents = 0 },
		"duplicate case":    func(f *DemandFixture) { f.Selection = append(f.Selection, f.Selection[0]) },
		"oracle explosion": func(f *DemandFixture) {
			for _, p := range f.Products {
				f.Selection[0].Candidates = append(f.Selection[0].Candidates, p.ID)
			}
		},
		"missing SKU": func(f *DemandFixture) { f.Selection[0].Candidates[0] = "missing" },
		"duplicate SKU": func(f *DemandFixture) {
			f.Selection[0].Candidates = append(f.Selection[0].Candidates, f.Selection[0].Candidates[0])
		},
		"null array":                func(f *DemandFixture) { f.Selection[0].State.Optional = nil },
		"no execution":              func(f *DemandFixture) { f.Execution = nil },
		"unknown case":              func(f *DemandFixture) { f.Execution[0].SelectionCase = "missing" },
		"unknown stage":             func(f *DemandFixture) { f.Execution[9].FaultStage = "other" },
		"unknown error":             func(f *DemandFixture) { f.Execution[9].Fault = "other" },
		"unbounded check":           func(f *DemandFixture) { f.Execution[0].ExpectedCalls = 3 },
		"foreign update":            func(f *DemandFixture) { f.Execution[0].Updates = []DemandProduct{f.Products[0]} },
		"category without revision": func(f *DemandFixture) { f.Execution[1].Updates[0].Category = "phone" },
		"search response fault":     func(f *DemandFixture) { f.Execution[9].Fault = "bad_contract" },
		"initial rollback":          func(f *DemandFixture) { f.Execution[17].FaultStage = "initial_check" },
	} {
		t.Run(name, func(t *testing.T) {
			var f DemandFixture
			require.NoError(t, json.Unmarshal(data, &f))
			mutate(&f)
			bad, err := json.Marshal(f)
			require.NoError(t, err)
			_, err = LoadDemand(bytes.NewReader(bad))
			require.Error(t, err)
		})
	}
}

func TestIndependentDemandAssessmentRejectsUnsafeSelfReportedFacts(t *testing.T) {
	s := demand.State{SchemaVersion: 1, BudgetCents: 100, MaxItems: 2, Required: []demand.Category{"keyboard", "mouse"}, Excluded: []demand.Category{"phone"}}
	products := []DemandProduct{{ID: "k", Category: "keyboard", PriceCents: 40, Stock: 2}, {ID: "m", Category: "mouse", PriceCents: 40, Stock: 2}, {ID: "p", Category: "phone", PriceCents: 10, Stock: 2}, {ID: "u", Category: "unknown", PriceCents: 1, Stock: 2}}
	items := func(ids ...int) []agent.BundleItem {
		var out []agent.BundleItem
		for _, i := range ids {
			p := products[i]
			out = append(out, agent.BundleItem{Id: p.ID, Name: "fixture " + p.ID, Category: string(p.Category), Source: "mall", PriceCents: p.PriceCents, Stock: p.Stock})
		}
		return out
	}
	good := assessDemand(s, products, items(0, 1), 80)
	require.True(t, good.TaskSuccess)
	require.Equal(t, 2, good.RequiredMet)
	partial := assessDemand(s, products, items(0), 40)
	require.False(t, partial.TaskSuccess)
	require.Equal(t, 1, partial.RequiredMet)
	require.Contains(t, partial.Violations, "missing_required")
	for _, tc := range []struct {
		name      string
		items     []agent.BundleItem
		total     int64
		violation string
	}{
		{"duplicate", items(0, 0), 80, "duplicate_sku"}, {"excluded", items(0, 2), 50, "excluded_category"},
		{"unknown", items(0, 3), 41, "unknown_with_exclusions"}, {"count", items(0, 1, 2), 90, "item_limit"},
		{"incorrect sum", items(0, 1), 1, "incorrect_total"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := assessDemand(s, products, tc.items, tc.total)
			require.False(t, a.TaskSuccess)
			require.Zero(t, a.RequiredMet)
			require.Contains(t, a.Violations, tc.violation)
		})
	}
	for _, field := range []string{"id", "price", "stock", "name", "category", "source"} {
		t.Run(field, func(t *testing.T) {
			bad := items(0, 1)
			switch field {
			case "id":
				bad[0].Id = "foreign"
			case "price":
				bad[0].PriceCents = 1
			case "stock":
				bad[0].Stock++
			case "name":
				bad[0].Name = "forged"
			case "category":
				bad[0].Category = "phone"
			case "source":
				bad[0].Source = "demo"
			}
			a := assessDemand(s, products, bad, 80)
			require.False(t, a.TaskSuccess)
			require.Contains(t, a.Violations, "snapshot_fact_mismatch")
		})
	}
	empty := assessDemand(s, products, nil, 0)
	require.False(t, empty.TaskSuccess)
	require.Empty(t, empty.Violations)
	// Scoring penalties must not invent a second, factually false root cause.
	onlyKeyboard := s
	onlyKeyboard.Required = []demand.Category{"keyboard"}
	excluded := assessDemand(onlyKeyboard, products, items(0, 2), 50)
	require.Zero(t, excluded.RequiredMet)
	require.Contains(t, excluded.Violations, "excluded_category")
	require.NotContains(t, excluded.Violations, "missing_required")
	noApplicable := summarizeDemandSelection([]DemandSelectionOutcome{{DemandAssessment: empty, Oracle: DemandOracle{}}}, []DemandSelectionCase{{}})
	require.Nil(t, noApplicable.SatisfiableSuccess.Value)
	require.Nil(t, noApplicable.HardViolationRate.Value)
}

func TestDemandOracleKnownAnswersAndContext(t *testing.T) {
	d := loadDemandTest(t)
	for _, c := range d.fixture.Selection {
		o, err := demandOracle(context.Background(), c.State, d.products(c.Candidates))
		require.NoError(t, err)
		require.Equal(t, (1<<len(c.Candidates))-1, o.Subsets)
		switch c.ID {
		case "greedy-trap":
			require.Equal(t, []string{"cheap-key", "mouse"}, o.BestIDs)
		case "narrow-pruning-trap":
			require.Equal(t, []string{"b-cheap-key", "c-required-mouse"}, o.BestIDs)
		case "explicit-value":
			require.Equal(t, []string{"cheap-key"}, o.BestIDs)
		case "prerecorded-rank":
			require.Equal(t, []string{"premium-key"}, o.BestIDs)
		case "empty-snapshot", "budget-infeasible", "missing-category", "all-excluded":
			require.False(t, o.Feasible)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := demandOracle(ctx, d.fixture.Selection[0].State, d.fixture.Products[:3])
	require.ErrorIs(t, err, context.Canceled)
}

func TestDemandOracleMatchesWideEnumerationOnSeededSnapshots(t *testing.T) {
	rng := rand.New(rand.NewSource(4301))
	for trial := 0; trial < 128; trial++ {
		var products []DemandProduct
		for i, n := 0, 1+rng.Intn(8); i < n; i++ {
			products = append(products, DemandProduct{ID: fmt.Sprintf("s%d", i), Category: []demand.Category{"keyboard", "mouse", "lighting", "headphones", "unknown"}[rng.Intn(5)],
				Revision: 3, PriceCents: int64(1 + rng.Intn(70)), Stock: 1, KeywordRank: rng.Intn(64), VectorRank: rng.Intn(64)})
		}
		state := demand.State{SchemaVersion: 1, BudgetCents: int64(1 + rng.Intn(200)), MaxItems: 4,
			Required: []demand.Category{"keyboard", "mouse"}, Optional: []demand.Category{"lighting"}, Excluded: []demand.Category{"headphones"}}
		if trial%2 == 0 {
			state.Preferences = []demand.Preference{"value"}
		}
		oracle, err := demandOracle(context.Background(), state, products)
		require.NoError(t, err)
		out, err := runDemandSelection(context.Background(), DemandStrategy{"oracle_test", 32, 128, 65536, 1000}, DemandSelectionCase{State: state}, products, oracle)
		require.NoError(t, err)
		require.Empty(t, out.GateProblems, "trial %d", trial)
		require.Equal(t, oracle.Feasible, out.TaskSuccess, "trial %d", trial)
		if oracle.Feasible {
			require.True(t, out.OracleMatch, "trial %d: %+v", trial, out)
		}
	}
}

func TestDemandArchivedReportMatchesFixedInputsAndMechanics(t *testing.T) {
	d := loadDemandTest(t)
	data, err := os.ReadFile("../../testdata/eval/demand-baseline.v1/report.json")
	require.NoError(t, err)
	var archived DemandReport
	require.NoError(t, json.Unmarshal(data, &archived))
	md, err := os.ReadFile("../../testdata/eval/demand-baseline.v1/report.md")
	require.NoError(t, err)
	require.Equal(t, DemandMarkdown(archived), string(md))
	require.Equal(t, d.sha256, archived.Metadata.DatasetSHA256)
	current, err := RunDemand(context.Background(), d, archived.Metadata.CodeRevision)
	require.NoError(t, err)
	// Host/runtime and measured time can differ; never ignore quality, scope,
	// work counts, expectations, protocol hash or input fingerprints.
	archived.Metadata.GoVersion, archived.Metadata.OS, archived.Metadata.Arch, archived.Metadata.GOMAXPROCS = current.Metadata.GoVersion, current.Metadata.OS, current.Metadata.Arch, current.Metadata.GOMAXPROCS
	scrubDemandTiming(&archived)
	scrubDemandTiming(&current)
	require.Equal(t, archived, current)
}

func FuzzDemandFixture(f *testing.F) {
	data, err := os.ReadFile("../../testdata/eval/demand.v1.json")
	require.NoError(f, err)
	f.Add(data)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version":1,"version":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := LoadDemand(bytes.NewReader(data))
		if err == nil {
			require.NoError(t, d.fixture.validate())
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), d.sha256)
		}
	})
}

func BenchmarkDemandSelection(b *testing.B) {
	for _, n := range []int{8, 32} {
		var products []DemandProduct
		for i := 0; i < n; i++ {
			products = append(products, DemandProduct{ID: fmt.Sprintf("sku-%02d", i), Category: []demand.Category{"keyboard", "mouse", "lighting"}[i%3], Revision: 1, PriceCents: int64(10 + i), Stock: 2})
		}
		candidates := demandCandidates(products)
		catalog, err := demand.NewMallCatalog(candidates)
		require.NoError(b, err)
		state := demand.State{SchemaVersion: 1, BudgetCents: 200, MaxItems: 3, Required: []demand.Category{"keyboard", "mouse"}, Optional: []demand.Category{"lighting"}}
		for _, strategy := range demandStrategies() {
			b.Run(fmt.Sprintf("n%d/%s", n, strategy.ID), func(b *testing.B) {
				b.ReportAllocs()
				if strategy.ID == "legacy_greedy" {
					s := recommend.NewBundleSelector()
					intent := agent.Intent{BudgetCents: state.BudgetCents, MaxItems: state.MaxItems}
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						items, _ := s.Select(candidates, intent)
						if len(items) == 0 {
							b.Fatal("empty baseline")
						}
					}
				} else {
					s, err := beam.New(strategy.config())
					require.NoError(b, err)
					var out beam.Result
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						out, err = s.Select(context.Background(), state, catalog, candidates)
						if err != nil || out.Stats.TimeBudgetReached {
							b.Fatalf("inconclusive: %v", err)
						}
					}
					b.ReportMetric(float64(out.Stats.Expansions), "expansions/op")
				}
			})
		}
	}
}
