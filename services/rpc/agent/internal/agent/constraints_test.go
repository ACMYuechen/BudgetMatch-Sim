package agent

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func validResult() *Result {
	return &Result{
		Candidates:      []ProductCandidate{{Id: "a", Name: "keyboard", Source: "mall", Category: "office", PriceCents: 100, Stock: 2}},
		Items:           []BundleItem{{Id: "a", Name: "keyboard", Source: "mall", Category: "office", PriceCents: 100, Stock: 2}},
		TotalPriceCents: 100,
	}
}

func TestConstraintsValidateResult(t *testing.T) {
	limits := Constraints{BudgetCents: 200, MaxItems: 2}
	if err := limits.ValidateResult(validResult()); err != nil {
		t.Fatal(err)
	}
	if err := limits.ValidateResult(&Result{}); err != nil {
		t.Fatalf("empty result must be valid: %v", err)
	}
	if err := limits.ValidateResult(nil); !errors.Is(err, ErrUnsafeResult) {
		t.Fatalf("nil result: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Result)
	}{
		{"no evidence", func(r *Result) { r.Candidates = nil }},
		{"unknown SKU", func(r *Result) { r.Items[0].Id = "unknown" }},
		{"invented price", func(r *Result) { r.Items[0].PriceCents = 1; r.TotalPriceCents = 1 }},
		{"invented name", func(r *Result) { r.Items[0].Name = "invented" }},
		{"invented stock", func(r *Result) { r.Items[0].Stock = 999 }},
		{"wrong total", func(r *Result) { r.TotalPriceCents++ }},
		{"duplicate", func(r *Result) { r.Items = append(r.Items, r.Items[0]); r.TotalPriceCents = 200 }},
		{"too many", func(r *Result) { r.Items = append(r.Items, r.Items[0], r.Items[0]) }},
		{"negative price", func(r *Result) {
			r.Items[0].PriceCents = -100
			r.Candidates[0].PriceCents = -100
			r.TotalPriceCents = -100
		}},
		{"oversized price", func(r *Result) {
			r.Items[0].PriceCents = math.MaxInt64
			r.Candidates[0].PriceCents = math.MaxInt64
			r.TotalPriceCents = math.MaxInt64
		}},
		{"over budget", func(r *Result) {
			r.Items[0].PriceCents = 201
			r.Candidates[0].PriceCents = 201
			r.TotalPriceCents = 201
		}},
		{"latest snapshot unavailable", func(r *Result) {
			r.Candidates = append(r.Candidates, ProductCandidate{Id: "a", PriceCents: 100, Stock: 0})
		}},
		{"NaN", func(r *Result) { r.Items[0].Score = math.NaN() }},
		{"infinity", func(r *Result) { r.Items[0].Score = math.Inf(1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := validResult()
			tc.mutate(result)
			if err := limits.ValidateResult(result); !errors.Is(err, ErrUnsafeResult) {
				t.Fatalf("accepted unsafe result: %+v, err=%v", result, err)
			}
		})
	}
}

func TestConstraintsMustBeResolved(t *testing.T) {
	for _, limits := range []Constraints{{}, {BudgetCents: -1, MaxItems: 1}, {BudgetCents: MaxBudgetCents + 1, MaxItems: 1}, {BudgetCents: 100, MaxItems: 11}} {
		if _, _, err := limits.Restrict(1, 1); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted invalid base constraints: %+v, %v", limits, err)
		}
		if err := limits.ValidateResult(&Result{}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("validated using invalid constraints: %+v, %v", limits, err)
		}
	}
}

func TestCandidateEvidenceIsNotSerialized(t *testing.T) {
	result := validResult()
	result.Candidates[0].Tags = []string{"private-evidence-only"}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Candidates") || strings.Contains(string(data), "private-evidence-only") {
		t.Fatalf("internal evidence leaked: %s", data)
	}
}

func TestNormalizeCandidatesDoesNotAliasInput(t *testing.T) {
	input := []ProductCandidate{{Id: "a", PriceCents: 1, Stock: 1, Tags: []string{"original"}}}
	result := NormalizeCandidates(input)
	result[0].PriceCents = 20
	result[0].Tags[0] = "changed"
	if input[0].PriceCents != 1 || input[0].Tags[0] != "original" {
		t.Fatal("normalization aliases input")
	}
}
