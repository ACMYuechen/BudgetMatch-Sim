package demand

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"github.com/stretchr/testify/require"
)

func demoCatalogData(t *testing.T) ([]byte, DatasetBinding) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/demand/categories.v1.json")
	require.NoError(t, err)
	source, err := os.ReadFile("../../testdata/eval/retrieval.v1.json")
	require.NoError(t, err)
	var fixture struct {
		Version  string `json:"version"`
		Products []struct {
			ID        string `json:"id"`
			ProductID string `json:"product_id"`
		} `json:"products"`
	}
	require.NoError(t, json.Unmarshal(source, &fixture))
	var mapping catalogFile
	require.NoError(t, json.Unmarshal(data, &mapping))
	// Validate every demo mapping against the bound source's exact identities.
	require.Len(t, mapping.Entries, len(fixture.Products))
	for i, entry := range mapping.Entries {
		require.Equal(t, fixture.Products[i].ID, entry.SKUID)
		require.Equal(t, fixture.Products[i].ProductID, entry.ProductID)
	}
	return data, DatasetBinding{Version: fixture.Version, SHA256: fmt.Sprintf("%x", sha256.Sum256(source))}
}

func demoCatalog(t *testing.T) *Catalog {
	t.Helper()
	data, binding := demoCatalogData(t)
	c, err := LoadDemoCatalog(bytes.NewReader(data), binding)
	require.NoError(t, err)
	return c
}

func demoCandidate(id string, price int64) agent.ProductCandidate {
	return agent.ProductCandidate{Id: id, Name: "untrusted descriptive text", Category: "PRIVATE invented category", PriceCents: price, Stock: 1,
		Evidence: agent.CandidateEvidence{Source: agent.RetrievalDemo, State: agent.VerificationDemo, ProductID: "p-" + id}}
}

func TestDemoCatalogRequiresExactIdentityAndNeverClassifiesLiveCandidates(t *testing.T) {
	c := demoCatalog(t)
	data, binding := demoCatalogData(t)
	meta, hash := c.Metadata()
	require.Equal(t, binding, meta.DatasetBinding)
	require.Equal(t, "pending_human_review", meta.AnnotationStatus)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), hash)
	meta.Version = "mutated"
	got := c.Classify(demoCandidate("k1", 100))
	require.Equal(t, Keyboard, got.Category)
	require.Equal(t, "mapped_demo", got.Reason)
	require.Equal(t, "demo_categories_v1", got.MappingVersion)
	require.Equal(t, hash, got.MappingSHA256)
	for _, tc := range []struct {
		name, reason string
		mutate       func(*agent.ProductCandidate)
	}{
		{"unmapped", "unmapped_sku", func(p *agent.ProductCandidate) {
			p.Id = "missing"
			p.Name = "keyboard"
			p.Category = "keyboard"
			p.Tags = []string{"keyboard"}
		}},
		{"wrong parent", "identity_mismatch", func(p *agent.ProductCandidate) { p.Evidence.ProductID = "wrong" }},
		{"keyword", "not_demo_source", func(p *agent.ProductCandidate) { p.Evidence.Source = agent.RetrievalMallKeyword }},
		{"vector", "not_demo_source", func(p *agent.ProductCandidate) { p.Evidence.Source = agent.RetrievalMallVector }},
		{"unknown source", "not_demo_source", func(p *agent.ProductCandidate) { p.Evidence.Source = agent.RetrievalUnknown }},
		{"not demo state", "not_demo_source", func(p *agent.ProductCandidate) { p.Evidence.State = agent.VerificationChecked }},
		{"explicit unknown", "unclassified_demo", func(p *agent.ProductCandidate) { p.Id = "b1"; p.Evidence.ProductID = "p-b1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := demoCandidate("k1", 100)
			tc.mutate(&p)
			got := c.Classify(p)
			require.Equal(t, Unknown, got.Category)
			require.Equal(t, tc.reason, got.Reason)
		})
	}
	var absent *Catalog
	require.Equal(t, Unknown, absent.Classify(demoCandidate("k1", 100)).Category)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if c.Classify(demoCandidate("k1", 100)) != got {
					t.Error("shared mapping changed")
				}
			}
		}()
	}
	wg.Wait()
}

func TestDemoCatalogRejectsUnboundOrAmbiguousData(t *testing.T) {
	data, binding := demoCatalogData(t)
	for name, mutate := range map[string]func(*catalogFile){
		"version":          func(f *catalogFile) { f.Version = "" },
		"taxonomy":         func(f *catalogFile) { f.TaxonomyVersion = "other" },
		"source":           func(f *catalogFile) { f.Provenance = "production" },
		"review":           func(f *catalogFile) { f.AnnotationStatus = "accepted" },
		"binding version":  func(f *catalogFile) { f.DatasetBinding.Version = "other" },
		"binding hash":     func(f *catalogFile) { f.DatasetBinding.SHA256 = strings.Repeat("0", 64) },
		"empty":            func(f *catalogFile) { f.Entries = nil },
		"too many":         func(f *catalogFile) { f.Entries = make([]CategoryEntry, agent.MaxCandidateIDs+1) },
		"duplicate":        func(f *catalogFile) { f.Entries = append(f.Entries, f.Entries[0]) },
		"bad identity":     func(f *catalogFile) { f.Entries[0].SKUID = " " },
		"long identity":    func(f *catalogFile) { f.Entries[0].SKUID = strings.Repeat("s", 65) },
		"missing parent":   func(f *catalogFile) { f.Entries[0].ProductID = "" },
		"unknown category": func(f *catalogFile) { f.Entries[0].Category = "PRIVATE" },
		"missing category": func(f *catalogFile) { f.Entries[0].Category = "" },
	} {
		t.Run(name, func(t *testing.T) {
			var f catalogFile
			require.NoError(t, json.Unmarshal(data, &f))
			mutate(&f)
			raw, err := json.Marshal(f)
			require.NoError(t, err)
			_, err = LoadDemoCatalog(bytes.NewReader(raw), binding)
			require.ErrorIs(t, err, ErrInvalid)
			require.NotContains(t, err.Error(), "PRIVATE")
		})
	}
	for _, raw := range []string{
		string(data) + ` {}`, strings.Replace(string(data), `"category":"keyboard"`, `"category":"keyboard","category":"mouse"`, 1),
		strings.Replace(string(data), `"category":"keyboard"`, `"category":"keyboard","CATEGORY":"mouse"`, 1),
		strings.Replace(string(data), `"category":"keyboard"`, `"category":"keyboard","\u0063ategory":"mouse"`, 1),
		strings.Replace(string(data), `"category":"keyboard"`, `"category":"keyboard","secret":"PRIVATE"`, 1),
		strings.Repeat(" ", MaxCatalogBytes+1),
	} {
		_, err := LoadDemoCatalog(strings.NewReader(raw), binding)
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err := LoadDemoCatalog(bytes.NewReader(data), DatasetBinding{})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestDemandSelectionAssessmentUsesMappingNotDescriptiveFields(t *testing.T) {
	c := demoCatalog(t)
	s := initialState(t)
	s.Required, s.Optional, s.Excluded = []Category{Keyboard, Mouse}, []Category{Lighting}, []Category{Accessories}
	good := []agent.ProductCandidate{demoCandidate("k1", 100), demoCandidate("k3", 300), demoCandidate("k5", 500)}
	out, err := AssessSelection(s, c, good)
	require.NoError(t, err)
	require.True(t, out.ConstraintsSatisfied)
	require.EqualValues(t, 900, out.TotalPriceCents)
	require.Equal(t, []Category{Keyboard, Mouse}, out.CoveredRequired)
	require.Equal(t, []Category{Lighting}, out.CoveredOptional)
	for _, tc := range []struct {
		name      string
		selected  []agent.ProductCandidate
		missing   []Category
		violation string
	}{
		{"missing", good[:1], []Category{Mouse}, ""},
		{"empty", nil, []Category{Keyboard, Mouse}, ""},
		{"noise", []agent.ProductCandidate{good[0], good[1], demoCandidate("n1", 50)}, nil, "excluded_category"},
		{"unknown", []agent.ProductCandidate{good[0], good[1], demoCandidate("b5", 100)}, nil, "unknown_category_with_exclusions"},
		{"duplicate", []agent.ProductCandidate{good[0], good[0], good[1]}, nil, "duplicate_sku"},
		{"budget", []agent.ProductCandidate{good[0], demoCandidate("k3", 1000)}, nil, "over_budget"},
		{"zero price", []agent.ProductCandidate{demoCandidate("k1", 0), good[1]}, []Category{Keyboard}, "invalid_candidate"},
		{"too many", append(append([]agent.ProductCandidate{}, good...), demoCandidate("k2", 50)), nil, "too_many_items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AssessSelection(s, c, tc.selected)
			require.NoError(t, err)
			require.False(t, got.ConstraintsSatisfied)
			require.ElementsMatch(t, tc.missing, got.MissingRequired)
			if tc.violation != "" {
				require.Contains(t, got.Violations, tc.violation)
			}
		})
	}
	// A matching name/category/tag, or a live-source SKU with the same ID,
	// cannot manufacture the missing hard requirement from demo annotations.
	live := demoCandidate("k1", 100)
	live.Category, live.Name, live.Tags = "keyboard", "keyboard", []string{"keyboard"}
	live.Evidence.Source = agent.RetrievalMallKeyword
	out, err = AssessSelection(s, c, []agent.ProductCandidate{live, good[1]})
	require.NoError(t, err)
	require.Equal(t, []Category{Keyboard}, out.MissingRequired)
	require.Equal(t, []string{"k1"}, out.UnknownSKUs)
	for _, input := range [][]agent.ProductCandidate{make([]agent.ProductCandidate, agent.MaxCandidateIDs+1)} {
		_, err := AssessSelection(s, c, input)
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err = AssessSelection(s, nil, good)
	require.ErrorIs(t, err, ErrInvalid)
}
