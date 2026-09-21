package demandexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/recommend/beam"
	"github.com/stretchr/testify/require"
)

type sourceFuncs struct {
	search func(context.Context, agent.Intent) ([]agent.ProductCandidate, error)
	check  func(context.Context, []agent.ProductCandidate) (Batch, error)
}

func (s sourceFuncs) Search(ctx context.Context, i agent.Intent) ([]agent.ProductCandidate, error) {
	return s.search(ctx, i)
}
func (s sourceFuncs) Recheck(ctx context.Context, p []agent.ProductCandidate) (Batch, error) {
	return s.check(ctx, p)
}

func demoIntent() agent.Intent {
	return agent.Intent{BudgetCents: 40000, MaxItems: 3, Preferences: []string{"quiet"},
		Demand: &agent.DemandState{SchemaVersion: 1, Required: []string{"keyboard", "mouse"}}}
}

func checked(candidates []agent.ProductCandidate) Batch {
	batch := Batch{Candidates: cloneCandidates(candidates), CheckedAtMs: time.Now().UnixMilli()}
	for i := range batch.Candidates {
		batch.Candidates[i].Evidence.VerifiedAtUnixMs = batch.CheckedAtMs
	}
	return batch
}

func TestBuiltinDemoExecutionIsFactualAndOwned(t *testing.T) {
	e, err := NewBuiltinDemo()
	require.NoError(t, err)
	intent := demoIntent()
	out, err := e.Run(context.Background(), intent)
	require.NoError(t, err)
	require.Equal(t, beam.Complete, out.Status)
	require.Equal(t, int64(35800), out.TotalPriceCents)
	require.Len(t, out.Items, 2)
	require.Equal(t, []string{"keyboard", "mouse"}, out.Execution.CoveredRequired)
	require.Equal(t, []string{"quiet"}, out.Execution.UnscoredPreferences)
	require.Equal(t, beam.SnapshotScope, out.Execution.Scope)
	require.Contains(t, out.Summary, "非实时商城库存")
	require.Positive(t, out.Execution.SnapshotCheckedAtUnixMs)
	require.Len(t, out.Execution.MappingSHA256, 64)
	data, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(data), `"Window"`)
	require.NotContains(t, string(data), `"Candidates"`)
	require.NotContains(t, string(data), "candidate.verify")
	out.Intent.Demand.Required[0] = "phone"
	out.Candidates[0].Tags[0] = "MUTATION"
	require.Equal(t, "keyboard", intent.Demand.Required[0])
	again, err := e.Run(context.Background(), intent)
	require.NoError(t, err)
	require.Equal(t, int64(35800), again.TotalPriceCents)
	require.NotContains(t, again.Candidates[0].Tags, "MUTATION")
	another, err := NewBuiltinDemo()
	require.NoError(t, err)
	_, digest := another.catalog.Metadata()
	require.Equal(t, out.Execution.MappingSHA256, digest)
}

func TestRecheckPriceChangeReselectsAndUnavailableCannotPartiallySucceed(t *testing.T) {
	for _, scenario := range []string{"price_up", "price_down", "unavailable", "reorder"} {
		t.Run(scenario, func(t *testing.T) {
			e, err := NewBuiltinDemo()
			require.NoError(t, err)
			source := e.source
			calls := 0
			e.source = sourceFuncs{search: source.Search, check: func(ctx context.Context, input []agent.ProductCandidate) (Batch, error) {
				calls++
				batch := checked(input)
				for i, c := range batch.Candidates {
					if c.Id == "mock_keyboard_001" {
						switch scenario {
						case "price_up":
							batch.Candidates[i].PriceCents = 39900
						case "price_down":
							batch.Candidates[i].PriceCents = 100
						case "unavailable":
							batch.Unavailable = []string{c.Id}
							batch.Candidates = append(batch.Candidates[:i], batch.Candidates[i+1:]...)
						}
						break
					}
				}
				slices.Reverse(batch.Candidates)
				return batch, nil
			}}
			out, err := e.Run(context.Background(), demoIntent())
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			if scenario == "price_up" || scenario == "unavailable" {
				require.Equal(t, beam.NoFeasibleBundle, out.Status)
				require.Empty(t, out.Items)
				require.Zero(t, out.TotalPriceCents)
				require.Equal(t, []string{"keyboard", "mouse"}, out.Execution.MissingRequired)
			} else {
				require.Equal(t, beam.Complete, out.Status)
				want := int64(35800)
				if scenario == "price_down" {
					want = 10000
				}
				require.Equal(t, want, out.TotalPriceCents)
			}
		})
	}
}

func TestIncompleteOrForgedRecheckFailsClosed(t *testing.T) {
	mutations := map[string]func(*Batch){
		"missing":               func(b *Batch) { b.Candidates = b.Candidates[1:] },
		"extra":                 func(b *Batch) { b.Candidates = append(b.Candidates, b.Candidates[0]) },
		"duplicate":             func(b *Batch) { b.Candidates[1] = b.Candidates[0] },
		"unknown":               func(b *Batch) { b.Candidates[0].Id = "not-allowed" },
		"parent":                func(b *Batch) { b.Candidates[0].Evidence.ProductID = "forged" },
		"live_source":           func(b *Batch) { b.Candidates[0].Evidence.Source = agent.RetrievalMallKeyword },
		"live_state":            func(b *Batch) { b.Candidates[0].Evidence.State = agent.VerificationChecked },
		"timestamp":             func(b *Batch) { b.Candidates[0].Evidence.VerifiedAtUnixMs = 1 },
		"stale":                 func(b *Batch) { b.CheckedAtMs = 1 },
		"future":                func(b *Batch) { b.CheckedAtMs = time.Now().Add(time.Hour).UnixMilli() },
		"ranking":               func(b *Batch) { b.Candidates[0].Evidence.Ranking.FusionScore = math.NaN() },
		"price":                 func(b *Batch) { b.Candidates[0].PriceCents = 0 },
		"stock":                 func(b *Batch) { b.Candidates[0].Stock = 0 },
		"payload_bound":         func(b *Batch) { b.Candidates[0].Name = strings.Repeat("x", beam.MaxInputBytes+1) },
		"tags_bound":            func(b *Batch) { b.Candidates[0].Tags = make([]string, beam.MaxTagsPerSKU+1) },
		"unavailable_duplicate": func(b *Batch) { b.Unavailable = []string{b.Candidates[1].Id}; b.Candidates = b.Candidates[1:] },
		"unavailable_unknown":   func(b *Batch) { b.Unavailable = []string{"unknown"}; b.Candidates = b.Candidates[1:] },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			e, err := NewBuiltinDemo()
			require.NoError(t, err)
			source := e.source
			e.source = sourceFuncs{search: source.Search, check: func(_ context.Context, p []agent.ProductCandidate) (Batch, error) {
				batch := checked(p)
				mutate(&batch)
				return batch, nil
			}}
			out, err := e.Run(context.Background(), demoIntent())
			require.ErrorIs(t, err, agent.ErrUnsafeResult)
			require.Nil(t, out)
		})
	}
}

func TestRecheckReceivesIndependentTwoSecondDeadline(t *testing.T) {
	e, err := NewBuiltinDemo()
	require.NoError(t, err)
	source := e.source
	e.source = sourceFuncs{search: source.Search, check: func(ctx context.Context, p []agent.ProductCandidate) (Batch, error) {
		deadline, exists := ctx.Deadline()
		require.True(t, exists)
		require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
		return checked(p), nil
	}}
	out, err := e.Run(context.Background(), demoIntent())
	require.NoError(t, err)
	require.Equal(t, beam.Complete, out.Status)
}

func TestExecutionCancellationAndNoFallback(t *testing.T) {
	for _, phase := range []string{"before", "search", "check", "check_error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e, err := NewBuiltinDemo()
			require.NoError(t, err)
			base := e.source
			searches, checks := 0, 0
			e.source = sourceFuncs{search: func(ctx context.Context, intent agent.Intent) ([]agent.ProductCandidate, error) {
				searches++
				if phase == "search" {
					cancel()
				}
				// Input mutation cannot weaken the executor's accepted state.
				intent.Demand.Required[0] = "phone"
				return base.Search(ctx, intent)
			}, check: func(_ context.Context, p []agent.ProductCandidate) (Batch, error) {
				checks++
				if phase == "check_error" {
					return Batch{}, errors.New("dependency unavailable")
				}
				cancel()
				return checked(p), nil
			}}
			if phase == "before" {
				cancel()
			}
			out, err := e.Run(ctx, demoIntent())
			require.Error(t, err)
			require.Nil(t, out)
			if phase != "check_error" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.LessOrEqual(t, searches, 1)
			require.LessOrEqual(t, checks, 1)
		})
	}
}

func TestExecutionBoundsAndNoCandidateCheck(t *testing.T) {
	e, err := NewBuiltinDemo()
	require.NoError(t, err)
	_, err = New(e.source, e.catalog, beam.Config{MaxCandidates: 33})
	require.ErrorIs(t, err, beam.ErrConfig)
	_, err = New(e.source, nil, beam.Config{})
	require.Error(t, err)
	_, err = e.Run(context.Background(), agent.Intent{})
	require.ErrorIs(t, err, agent.ErrInvalidInput)
	base := e.source
	e.source = sourceFuncs{search: func(ctx context.Context, i agent.Intent) ([]agent.ProductCandidate, error) {
		p, err := base.Search(ctx, i)
		for n := range p {
			p[n].Evidence.Source = agent.RetrievalMallKeyword
		}
		return p, err
	}, check: func(context.Context, []agent.ProductCandidate) (Batch, error) {
		t.Fatal("non-demo check")
		return Batch{}, nil
	}}
	out, err := e.Run(context.Background(), demoIntent())
	require.NoError(t, err)
	require.Equal(t, beam.NoFeasibleBundle, out.Status)
	require.Zero(t, out.Execution.SnapshotCheckedAtUnixMs)
	limited, err := New(base, e.catalog, beam.Config{MaxExpansions: 1})
	require.NoError(t, err)
	out, err = limited.Run(context.Background(), demoIntent())
	require.NoError(t, err)
	require.True(t, out.Execution.SearchLimited)
	require.LessOrEqual(t, out.Execution.InitialExpansions, int32(1))
	require.LessOrEqual(t, out.Execution.FinalExpansions, int32(1))
}

func TestLargeCandidatePoolKeepsRecheckWithin32AndReselectsAlternative(t *testing.T) {
	var products []agent.ProductCandidate
	var entries []demand.CategoryEntry
	for i := 0; i < 48; i++ {
		id := fmt.Sprintf("sku-%02d", i)
		products = append(products, agent.ProductCandidate{Id: id, Name: id, PriceCents: int64(10 + i), Stock: 1,
			Evidence: agent.CandidateEvidence{Source: agent.RetrievalDemo, State: agent.VerificationDemo, ProductID: "p-" + id}})
		category := demand.Keyboard
		if i%2 != 0 {
			category = demand.Mouse
		}
		entries = append(entries, demand.CategoryEntry{SKUID: id, ProductID: "p-" + id, Category: category})
	}
	data, _ := json.Marshal(products)
	binding := demand.DatasetBinding{Version: "execution_test_v1", SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	encoded, err := json.Marshal(struct {
		demand.CatalogMetadata
		Entries []demand.CategoryEntry `json:"entries"`
	}{
		demand.CatalogMetadata{Version: "execution_categories_v1", TaxonomyVersion: demand.TaxonomyVersion,
			Provenance: "synthetic_demo", AnnotationStatus: "pending_human_review", DatasetBinding: binding}, entries})
	require.NoError(t, err)
	catalog, err := demand.LoadDemoCatalog(bytes.NewReader(encoded), binding)
	require.NoError(t, err)
	checks := 0
	source := sourceFuncs{search: func(context.Context, agent.Intent) ([]agent.ProductCandidate, error) {
		return cloneCandidates(products), nil
	},
		check: func(_ context.Context, p []agent.ProductCandidate) (Batch, error) {
			checks++
			require.Len(t, p, 32)
			batch := checked(p)
			for i := range batch.Candidates {
				if batch.Candidates[i].Id == "sku-00" {
					batch.Candidates[i].PriceCents = 1000
				}
			}
			return batch, nil
		}}
	e, err := New(source, catalog, beam.Config{})
	require.NoError(t, err)
	intent := demoIntent()
	intent.BudgetCents = 100
	out, err := e.Run(context.Background(), intent)
	require.NoError(t, err)
	require.Equal(t, beam.Complete, out.Status)
	require.Equal(t, int64(23), out.TotalPriceCents)
	require.Equal(t, "sku-01", out.Items[0].Id)
	require.Equal(t, "sku-02", out.Items[1].Id)
	require.Equal(t, 1, checks)
	require.True(t, out.Execution.SearchLimited)
}
