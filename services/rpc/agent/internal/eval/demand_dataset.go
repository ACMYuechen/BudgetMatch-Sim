package eval

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

const demandFixtureLimit = 128 << 10

// These are synthetic facts and prerecorded ranks, not user text or real Mall
// records. Rank replay only exercises the selector; Mall execution has no RRF.
type DemandProduct struct {
	ID          string          `json:"id"`
	Category    demand.Category `json:"category"`
	Revision    int64           `json:"revision"`
	PriceCents  int64           `json:"price_cents"`
	Stock       int64           `json:"stock"`
	Sold        int64           `json:"sold"`
	KeywordRank int             `json:"keyword_rank"`
	VectorRank  int             `json:"vector_rank"`
}

type DemandSelectionCase struct {
	ID         string       `json:"id"`
	State      demand.State `json:"state"`
	Candidates []string     `json:"candidates"`
}

type DemandExecutionCase struct {
	ID            string          `json:"id"`
	SelectionCase string          `json:"selection_case"`
	Updates       []DemandProduct `json:"updates"`
	FaultStage    string          `json:"fault_stage"`
	Fault         string          `json:"fault"`
	Expected      string          `json:"expected"`
	ExpectedCalls int             `json:"expected_check_calls"`
}

type DemandFixture struct {
	Version          string                `json:"version"`
	Provenance       string                `json:"provenance"`
	AnnotationStatus string                `json:"annotation_status"`
	Products         []DemandProduct       `json:"products"`
	Selection        []DemandSelectionCase `json:"selection"`
	Execution        []DemandExecutionCase `json:"execution"`
}

// Private ownership binds the immutable input and labels to the raw-file hash.
type DemandDataset struct {
	fixture DemandFixture
	sha256  string
}

func LoadDemand(r io.Reader) (DemandDataset, error) {
	data, err := io.ReadAll(io.LimitReader(r, demandFixtureLimit+1))
	if err != nil || len(data) > demandFixtureLimit || !utf8.Valid(data) || !uniqueRetrievalJSON(data) {
		return DemandDataset{}, errors.New("invalid or oversized demand fixture JSON")
	}
	var f DemandFixture
	if err := decodeStrict(data, &f); err != nil {
		return DemandDataset{}, errors.New("invalid demand fixture fields")
	}
	if err := f.validate(); err != nil {
		return DemandDataset{}, err
	}
	return DemandDataset{fixture: f, sha256: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

func (p DemandProduct) valid() bool {
	return safeID.MatchString(p.ID) && candidatecontract.ValidID("p-"+p.ID) &&
		candidatecontract.ValidDemandCategoryFact(string(p.Category), candidatecontract.DemandTaxonomyVersion, p.Revision) &&
		p.PriceCents >= 0 && p.PriceCents <= agent.MaxBudgetCents && p.Stock >= 0 && p.Sold >= 0 &&
		p.KeywordRank >= 0 && p.KeywordRank <= agent.MaxCandidateIDs && p.VectorRank >= 0 && p.VectorRank <= agent.MaxCandidateIDs
}

func (f DemandFixture) validate() error {
	bad := func() error {
		return errors.New("invalid demand fixture: require bounded synthetic facts, explicit arrays, valid references and fault expectations")
	}
	if !safeID.MatchString(f.Version) || f.Provenance != "synthetic_demand_snapshots" || f.AnnotationStatus != "pending_human_review" ||
		len(f.Products) < 1 || len(f.Products) > 64 || len(f.Selection) < 1 || len(f.Selection) > 128 || len(f.Execution) < 1 || len(f.Execution) > 128 {
		return bad()
	}
	products := map[string]DemandProduct{}
	for _, p := range f.Products {
		if !p.valid() || products[p.ID].ID != "" {
			return bad()
		}
		products[p.ID] = p
	}
	cases := map[string]DemandSelectionCase{}
	for _, c := range f.Selection {
		// Exhaustive enumeration is deliberately capped at 8 SKUs / 255 subsets.
		if !safeID.MatchString(c.ID) || cases[c.ID].ID != "" || c.Candidates == nil || len(c.Candidates) > 8 || c.State.Validate() != nil ||
			c.State.Required == nil || c.State.Optional == nil || c.State.Excluded == nil || c.State.Preferences == nil {
			return bad()
		}
		seen := map[string]bool{}
		for _, id := range c.Candidates {
			if products[id].ID == "" || seen[id] {
				return bad()
			}
			seen[id] = true
		}
		cases[c.ID] = c
	}
	seen := map[string]bool{}
	for _, c := range f.Execution {
		base, exists := cases[c.SelectionCase]
		if !safeID.MatchString(c.ID) || seen[c.ID] || !exists || c.Updates == nil || len(c.Updates) > len(base.Candidates) || c.ExpectedCalls < 0 || c.ExpectedCalls > 2 {
			return bad()
		}
		seen[c.ID] = true
		updated := map[string]bool{}
		for _, p := range c.Updates {
			old := products[p.ID]
			if !p.valid() || !slices.Contains(base.Candidates, p.ID) || updated[p.ID] ||
				p.KeywordRank != old.KeywordRank || p.VectorRank != old.VectorRank ||
				p.Revision > 0 && (p.Revision < old.Revision || p.Revision == old.Revision && p.Category != old.Category) {
				return bad()
			}
			updated[p.ID] = true
		}
		if c.Fault == "" {
			if c.FaultStage != "" || (c.Expected != "complete" && c.Expected != "no_feasible_bundle") {
				return bad()
			}
		} else {
			if !slices.Contains([]string{"search", "initial_check", "final_check"}, c.FaultStage) ||
				!slices.Contains([]string{"unavailable", "permission_denied", "deadline_exceeded", "canceled", "missing_fact", "bad_contract", "revision_rollback"}, c.Fault) ||
				!slices.Contains([]string{"unavailable", "permission_denied", "deadline_exceeded", "canceled", "unsafe_result"}, c.Expected) {
				return bad()
			}
			if (c.Fault == "missing_fact" || c.Fault == "bad_contract") && c.FaultStage == "search" ||
				c.Fault == "revision_rollback" && c.FaultStage != "final_check" {
				return bad()
			}
		}
	}
	return nil
}

func (d DemandDataset) products(ids []string) []DemandProduct {
	byID := map[string]DemandProduct{}
	for _, p := range d.fixture.Products {
		byID[p.ID] = p
	}
	out := make([]DemandProduct, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func demandIntent(s demand.State) agent.Intent {
	out := agent.Intent{BudgetCents: s.BudgetCents, MaxItems: s.MaxItems, Keywords: []string{"offline"},
		Demand: &agent.DemandState{SchemaVersion: s.SchemaVersion}}
	for _, c := range s.Required {
		out.Demand.Required = append(out.Demand.Required, string(c))
	}
	for _, c := range s.Optional {
		out.Demand.Optional = append(out.Demand.Optional, string(c))
	}
	for _, c := range s.Excluded {
		out.Demand.Excluded = append(out.Demand.Excluded, string(c))
	}
	for _, p := range s.Preferences {
		out.Preferences = append(out.Preferences, string(p))
	}
	return out
}
