package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

func fixture(t *testing.T) Dataset {
	t.Helper()
	sf, err := os.Open("../../testdata/eval/products.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()
	cf, err := os.Open("../../testdata/eval/cases.v1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer cf.Close()
	d, err := Load(sf, cf)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func miniDataset() Dataset {
	return Dataset{Snapshot: Snapshot{Version: "test-v1", Provenance: "synthetic", AnnotationStatus: "pending_human_review", Products: []Product{{ID: "sku_a", Name: "keyboard", Category: "keyboard", PriceCents: 100, Stock: 2, Active: true}}}, Cases: []Case{{ID: "case_a", Family: "family_a", Split: "dev", Scenario: "single", SnapshotVersion: "test-v1", Input: TurnInput{Query: "keyboard", BudgetCents: 100, MaxItems: 1}, Expected: Expected{Status: "completed", Feasibility: "satisfiable", BudgetCents: 100, MaxItems: 1, AcceptableSKUs: []string{"sku_a"}, RelevantSKUs: []string{"sku_a"}, Requirements: []Requirement{{ID: "keyboard", AnyOfSKUs: []string{"sku_a"}}}}}}}
}

func goodResult() *agent.Result {
	return &agent.Result{Intent: agent.Intent{BudgetCents: 100, MaxItems: 1}, Items: []agent.BundleItem{{Id: "sku_a", Name: "keyboard", Category: "keyboard", Source: "eval_snapshot", PriceCents: 100, Stock: 2}}, TotalPriceCents: 100}
}

func TestDatasetReferencesFamiliesAndFeasibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Dataset)
	}{
		{"duplicate product", func(d *Dataset) { d.Snapshot.Products = append(d.Snapshot.Products, d.Snapshot.Products[0]) }},
		{"duplicate case", func(d *Dataset) { d.Cases = append(d.Cases, d.Cases[0]) }},
		{"cross split family", func(d *Dataset) { c := d.Cases[0]; c.ID = "case_b"; c.Split = "holdout"; d.Cases = append(d.Cases, c) }},
		{"wrong version", func(d *Dataset) { d.Cases[0].SnapshotVersion = "other" }},
		{"duplicate input across families", func(d *Dataset) {
			c := d.Cases[0]
			c.ID = "case_b"
			c.Family = "other_family"
			c.Split = "holdout"
			d.Cases = append(d.Cases, c)
		}},
		{"unknown SKU", func(d *Dataset) { d.Cases[0].Expected.RelevantSKUs = []string{"unknown"} }},
		{"missing requirements", func(d *Dataset) { d.Cases[0].Expected.Requirements = nil }},
		{"false feasible", func(d *Dataset) { d.Cases[0].Expected.AcceptableSKUs = nil }},
		{"false unsatisfiable", func(d *Dataset) { d.Cases[0].Expected.Feasibility = "unsatisfiable" }},
		{"unavailable annotation", func(d *Dataset) { d.Snapshot.Products[0].Stock = 0 }},
		{"arbitrary fault", func(d *Dataset) { d.Cases[0].Fault = "external_model" }},
		{"wrong provenance", func(d *Dataset) { d.Snapshot.Provenance = "production" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := miniDataset()
			tc.change(&d)
			if d.Validate() == nil {
				t.Fatal("invalid dataset accepted")
			}
		})
	}
}

func TestLoadRejectsMalformedUnknownAndOversizeData(t *testing.T) {
	d := miniDataset()
	s, _ := json.Marshal(d.Snapshot)
	c, _ := json.Marshal(d.Cases[0])
	for _, tc := range []struct{ snapshot, cases string }{
		{string(s), string(c) + "\n\n"}, {string(s), string(c) + " {}"}, {string(s), `{"unknown":true}`},
		{string(s), strings.Repeat("x", 65<<10)}, {strings.Repeat("x", maxDatasetBytes+1), string(c)},
		{string(s) + " {}", string(c)}, {`{"version":"test-v1","unknown":true}`, string(c)},
	} {
		if _, err := Load(strings.NewReader(tc.snapshot), strings.NewReader(tc.cases)); err == nil {
			t.Fatal("bad JSONL accepted")
		}
	}
	got, err := Load(strings.NewReader(string(s)), strings.NewReader(string(c)+"\n"))
	if err != nil || len(got.SnapshotSHA256) != 64 || len(got.CasesSHA256) != 64 {
		t.Fatalf("load: %+v %v", got, err)
	}
}

func TestAssessUsesIndependentSnapshotAndExpectedLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*agent.Result)
		code   string
	}{
		{"unknown", func(r *agent.Result) { r.Items[0].Id = "forged" }, "snapshot_fact_mismatch"},
		{"price", func(r *agent.Result) { r.Items[0].PriceCents = 90; r.TotalPriceCents = 90 }, "snapshot_fact_mismatch"},
		{"intent", func(r *agent.Result) { r.Intent.BudgetCents = 999999 }, "resolved_limits_mismatch"},
		{"sum", func(r *agent.Result) { r.TotalPriceCents = 1 }, "incorrect_total"},
		{"duplicate", func(r *agent.Result) { r.Items = append(r.Items, r.Items[0]); r.TotalPriceCents = 200 }, "duplicate_sku"},
		{"negative", func(r *agent.Result) { r.Items[0].PriceCents = -1 }, "invalid_price_or_overflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := miniDataset()
			r := goodResult()
			tc.change(r)
			got := Assess(d.Snapshot, d.Cases[0], r, nil, nil)
			if !contains(got.Violations, tc.code) || got.TaskSuccess || got.RequirementsMet != 0 {
				t.Fatalf("not caught: %+v", got)
			}
		})
	}
	d := miniDataset()
	r := goodResult()
	r.Candidates = []agent.ProductCandidate{{Id: "forged", PriceCents: 1}}
	got := Assess(d.Snapshot, d.Cases[0], r, nil, []string{"sku_a", "sku_a"})
	if !got.TaskSuccess || got.RelevantHits != 1 {
		t.Fatalf("valid facts: %+v", got)
	}
}

func TestFeasibilityOracleMatchesExhaustiveSmallCatalogs(t *testing.T) {
	rng := rand.New(rand.NewSource(20260917))
	for range 200 {
		e := Expected{BudgetCents: int64(10 + rng.Intn(100)), MaxItems: int32(1 + rng.Intn(3))}
		products := map[string]Product{}
		for i := 0; i < 1+rng.Intn(7); i++ {
			id := fmt.Sprintf("sku_%d", i)
			e.AcceptableSKUs = append(e.AcceptableSKUs, id)
			products[id] = Product{ID: id, PriceCents: int64(1 + rng.Intn(60))}
		}
		for i := 0; i < 1+rng.Intn(3); i++ {
			r := Requirement{ID: fmt.Sprintf("req_%d", i)}
			for _, id := range e.AcceptableSKUs {
				if rng.Intn(2) == 0 {
					r.AnyOfSKUs = append(r.AnyOfSKUs, id)
				}
			}
			e.Requirements = append(e.Requirements, r)
		}
		want := false
		for mask := 1; mask < 1<<len(e.AcceptableSKUs); mask++ {
			var selected []string
			var total int64
			for i, id := range e.AcceptableSKUs {
				if mask&(1<<i) != 0 {
					selected = append(selected, id)
					total += products[id].PriceCents
				}
			}
			if len(selected) > int(e.MaxItems) || total > e.BudgetCents {
				continue
			}
			covered := 0
			for _, r := range e.Requirements {
				for _, id := range selected {
					if contains(r.AnyOfSKUs, id) {
						covered++
						break
					}
				}
			}
			if covered == len(e.Requirements) {
				want = true
				break
			}
		}
		if got := feasible(e, products); got != want {
			t.Fatalf("oracle mismatch: %+v got=%v want=%v", e, got, want)
		}
	}
}

func TestEmptyBundleCannotPassTaskMetric(t *testing.T) {
	d := miniDataset()
	r := goodResult()
	r.Items = nil
	r.TotalPriceCents = 0
	got := Assess(d.Snapshot, d.Cases[0], r, nil, nil)
	if len(got.Violations) != 0 || got.TaskSuccess || got.RequirementsMet != 0 {
		t.Fatalf("empty bundle metric: %+v", got)
	}
	got = Assess(d.Snapshot, d.Cases[0], nil, nil, nil)
	if len(got.Violations) == 0 {
		t.Fatal("nil result accepted")
	}
}

func TestMetricsKeepDenominatorsAndFallbackSeparate(t *testing.T) {
	d := miniDataset()
	ok := Assess(d.Snapshot, d.Cases[0], goodResult(), nil, []string{"sku_a"})
	ok.PersistenceOK, ok.ReplayChecked, ok.ReplayOK = true, true, true
	empty := ok
	empty.TaskSuccess = false
	empty.RequirementsMet = 0
	empty.RelevantHits = 0
	empty.Fallback = true
	s := Summarize([]CaseResult{ok, empty})
	if s.SatisfiableSuccess.Numerator != 1 || s.SatisfiableSuccess.Denominator != 2 || *s.RequirementCoverage.Value != 0.5 || *s.RecallAtK.Value != 0.5 || *s.FallbackRate.Value != 0.5 {
		t.Fatalf("wrong aggregation: %+v", s)
	}
	if Summarize(nil).RecallAtK.Value != nil || Summarize(nil).GatePassed {
		t.Fatal("empty sample misreported")
	}
	if percentile([]float64{4, 1, 3, 2}, 0.5) != 2 || percentile([]float64{4, 1, 3, 2}, 0.95) != 4 {
		t.Fatal("percentile definition changed")
	}
	empty.ReplayOK = false
	if Summarize([]CaseResult{empty}).GatePassed {
		t.Fatal("replay regression passed gate")
	}
}

func TestProviderFiltersWithoutLookingAtAnswers(t *testing.T) {
	d := miniDataset()
	inactive := d.Snapshot.Products[0]
	inactive.ID = "inactive"
	inactive.Active = false
	soldout := d.Snapshot.Products[0]
	soldout.ID = "soldout"
	soldout.Stock = 0
	expensive := d.Snapshot.Products[0]
	expensive.ID = "expensive"
	expensive.PriceCents = 200
	p := &snapshotProvider{products: append(d.Snapshot.Products, inactive, soldout, expensive), topK: 1}
	got, err := p.SearchProducts(context.Background(), tools.SearchProductsReq{Keywords: []string{"keyboard"}, BudgetCents: 100})
	if err != nil || len(got) != 1 || got[0].Id != "sku_a" {
		t.Fatalf("filters: %+v %v", got, err)
	}
	got, err = p.SearchProducts(context.Background(), tools.SearchProductsReq{Keywords: []string{"unmatched"}, BudgetCents: 100})
	if err != nil || len(got) != 0 {
		t.Fatal("no-match must stay empty")
	}
}

func TestCorpusRepeatableAndKnownGapsRemainVisible(t *testing.T) {
	d := fixture(t)
	if len(d.Cases) < 60 {
		t.Fatal("fewer than 60 cases")
	}
	a, err := Run(context.Background(), d, Options{Revision: "test"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(context.Background(), d, Options{Revision: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.Cases {
		a.Cases[i].Timing = Timing{}
		b.Cases[i].Timing = Timing{}
	}
	if !reflect.DeepEqual(a.Cases, b.Cases) {
		t.Fatal("non-timing results changed on repeat")
	}
	if a.Summary.HardViolationRequests != 0 || a.Summary.ReplaySuccess.Numerator != a.Summary.Completed || a.Summary.PersistenceSuccess.Numerator != len(d.Cases) {
		t.Fatalf("safety/replay regression: %+v", a.Summary)
	}
	if a.Baselines[1].Status != "not_run" || a.Baselines[2].Status != "not_run" || a.Summary.ModelCalls != 0 {
		t.Fatal("fake external model evidence")
	}
	// 基线允许真实质量缺口存在；测试评测器不等于全部评测用例通过。
	if a.Summary.SatisfiableSuccess.Denominator != 40 {
		t.Fatal("satisfiable denominator changed; review dataset version")
	}
	for _, c := range a.Cases {
		if !c.OutcomeMatched {
			t.Fatalf("outcome regression: %s", c.ID)
		}
	}
	if !a.Summary.GatePassed {
		t.Fatal("M2.2 fixed outcome regressions; gate must now pass")
	}
}

func TestRunSplitOptionsAndCancellation(t *testing.T) {
	d := fixture(t)
	for _, split := range []string{"dev", "holdout"} {
		r, err := Run(context.Background(), d, Options{Split: split})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range r.Cases {
			if c.Split != split {
				t.Fatal("split leak")
			}
		}
	}
	for _, opts := range []Options{{Split: "random"}, {TopK: -1}, {TopK: 257}} {
		if _, err := Run(context.Background(), d, opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, d, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMarkdownReportsUnrunAndFailures(t *testing.T) {
	r, err := Run(context.Background(), miniDataset(), Options{Revision: "test|revision\nunsafe"})
	if err != nil {
		t.Fatal(err)
	}
	md := Markdown(r)
	for _, want := range []string{"not_run", "待人工复核", "Recall@K", "test\\|revision unsafe", "1/1"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestArchivedBaselineMatchesInputsAndMetricDefinitions(t *testing.T) {
	d := fixture(t)
	data, err := os.ReadFile("../../testdata/eval/baseline.v1/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Metadata.SnapshotSHA256 != d.SnapshotSHA256 || r.Metadata.CasesSHA256 != d.CasesSHA256 {
		t.Fatal("v1 inputs changed without versioning the baseline")
	}
	if !reflect.DeepEqual(Summarize(r.Cases), r.Summary) {
		t.Fatal("archived metrics do not match their cases/definitions")
	}
	md, err := os.ReadFile("../../testdata/eval/baseline.v1/report.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(md) != Markdown(r) {
		t.Fatal("archived JSON and Markdown disagree")
	}
}
