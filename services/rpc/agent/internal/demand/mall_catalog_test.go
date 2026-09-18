package demand

import (
	"slices"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"github.com/stretchr/testify/require"
)

func classifiedCandidate(id, code string) agent.ProductCandidate {
	return agent.ProductCandidate{Id: id, PriceCents: 100, Stock: 1, Name: "untrusted", Category: "untrusted",
		Evidence: agent.CandidateEvidence{Source: agent.RetrievalMallKeyword, State: agent.VerificationChecked,
			ProductID: "p-" + id, VerifiedAtUnixMs: 1000,
			DemandCategory: agent.DemandCategoryEvidence{Code: code, TaxonomyVersion: candidatecontract.DemandTaxonomyVersion, Revision: 1}}}
}

func TestMallCatalogBindsImmutableClassificationsAndSeparateProvenance(t *testing.T) {
	input := []agent.ProductCandidate{classifiedCandidate("a", "keyboard"), classifiedCandidate("b", "unknown")}
	catalog, err := NewMallCatalog(input)
	require.NoError(t, err)
	metadata, digest := catalog.Metadata()
	require.Equal(t, "mall_checked_snapshot_only", catalog.Scope())
	require.Equal(t, candidatecontract.DemandTaxonomyVersion, metadata.TaxonomyVersion)
	require.Equal(t, "mall_managed_classification", metadata.Provenance)
	require.Len(t, digest, 64)
	reversed := slices.Clone(input)
	slices.Reverse(reversed)
	other, err := NewMallCatalog(reversed)
	require.NoError(t, err)
	_, otherDigest := other.Metadata()
	require.Equal(t, digest, otherDigest)
	for _, code := range []string{"keyboard", "mouse", "private"} {
		candidate := input[0]
		candidate.Category, candidate.Name, candidate.Tags = code, code, []string{code}
		require.Equal(t, Keyboard, catalog.Classify(candidate).Category)
	}
	for _, mutate := range []func(*agent.ProductCandidate){
		func(c *agent.ProductCandidate) {
			c.Evidence.Source = agent.RetrievalDemo
			c.Evidence.State = agent.VerificationDemo
		},
		func(c *agent.ProductCandidate) { c.Evidence.ProductID = "other" },
		func(c *agent.ProductCandidate) { c.Evidence.VerifiedAtUnixMs++ },
		func(c *agent.ProductCandidate) { c.Evidence.DemandCategory.Code = "mouse" },
		func(c *agent.ProductCandidate) { c.Evidence.DemandCategory.Revision++ },
	} {
		candidate := input[0]
		mutate(&candidate)
		require.False(t, catalog.AcceptsEvidence(candidate))
		require.Equal(t, Unknown, catalog.Classify(candidate).Category)
	}
	input[0].Evidence.DemandCategory.Code = "mouse"
	input[0].Evidence.DemandCategory.Revision++
	updated, err := NewMallCatalog(input)
	require.NoError(t, err)
	_, updatedDigest := updated.Metadata()
	require.NotEqual(t, digest, updatedDigest)
	require.Equal(t, Mouse, updated.Classify(input[0]).Category)
	require.False(t, catalog.AcceptsEvidence(input[0]), "caller mutation must not replace the old binding")
	demo := demoCatalog(t)
	require.False(t, demo.AcceptsEvidence(input[0]))
	empty, err := NewMallCatalog(nil)
	require.NoError(t, err)
	require.False(t, empty.AcceptsEvidence(input[0]))
}

func TestMallCatalogRejectsMissingOrInvalidFactsAndEnforcesUnknownRules(t *testing.T) {
	for _, mutate := range []func(*agent.ProductCandidate){
		func(c *agent.ProductCandidate) { c.Evidence.DemandCategory = agent.DemandCategoryEvidence{} },
		func(c *agent.ProductCandidate) { c.Evidence.DemandCategory.TaxonomyVersion = TaxonomyVersion },
		func(c *agent.ProductCandidate) { c.Evidence.DemandCategory.Revision = 0 },
		func(c *agent.ProductCandidate) { c.Evidence.State = agent.VerificationUnverified },
		func(c *agent.ProductCandidate) { c.Evidence.VerifiedAtUnixMs = 0 },
		func(c *agent.ProductCandidate) { c.Id = " " },
	} {
		candidate := classifiedCandidate("a", "keyboard")
		mutate(&candidate)
		_, err := NewMallCatalog([]agent.ProductCandidate{candidate})
		require.ErrorIs(t, err, ErrInvalid)
	}
	candidate := classifiedCandidate("a", "unknown")
	candidate.Evidence.DemandCategory.Revision = 0
	_, err := NewMallCatalog([]agent.ProductCandidate{candidate, candidate})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = NewMallCatalog(make([]agent.ProductCandidate, candidatecontract.MaxCandidates+1))
	require.ErrorIs(t, err, ErrInvalid)
	catalog, err := NewMallCatalog([]agent.ProductCandidate{candidate})
	require.NoError(t, err)
	state := State{SchemaVersion: SchemaVersion, BudgetCents: 300, MaxItems: 1, Required: []Category{Keyboard}}
	assessment, err := AssessSelection(state, catalog, []agent.ProductCandidate{candidate})
	require.NoError(t, err)
	require.False(t, assessment.ConstraintsSatisfied)
	require.Equal(t, []Category{Keyboard}, assessment.MissingRequired)
	state.Required, state.Excluded = nil, []Category{Phone}
	assessment, err = AssessSelection(state, catalog, []agent.ProductCandidate{candidate})
	require.NoError(t, err)
	require.Contains(t, assessment.Violations, "unknown_category_with_exclusions")
	state.Excluded = nil
	candidate.Evidence.State = agent.VerificationUnverified
	assessment, err = AssessSelection(state, catalog, []agent.ProductCandidate{candidate})
	require.NoError(t, err)
	require.Contains(t, assessment.Violations, "invalid_evidence")
	require.False(t, assessment.ConstraintsSatisfied, "broad demand still requires trusted facts")
}

func TestMallCatalogRejectsContradictorySPUOrMixedCheckTimes(t *testing.T) {
	a, b := classifiedCandidate("a", "keyboard"), classifiedCandidate("b", "keyboard")
	b.Evidence.ProductID = a.Evidence.ProductID
	_, err := NewMallCatalog([]agent.ProductCandidate{a, b})
	require.NoError(t, err, "multiple SKUs in one SPU are allowed when their classification agrees")
	for _, mutate := range []func(*agent.ProductCandidate){
		func(c *agent.ProductCandidate) { c.Evidence.DemandCategory.Code = "mouse" },
		func(c *agent.ProductCandidate) { c.Evidence.DemandCategory.Revision++ },
		func(c *agent.ProductCandidate) { c.Evidence.VerifiedAtUnixMs++ },
	} {
		changed := b
		mutate(&changed)
		_, err = NewMallCatalog([]agent.ProductCandidate{a, changed})
		require.ErrorIs(t, err, ErrInvalid)
	}
}
