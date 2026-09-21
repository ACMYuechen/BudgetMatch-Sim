package demand

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

type mallBinding struct {
	SKUID, ProductID string
	Category         agent.DemandCategoryEvidence
	CheckedAt        int64
}

// NewMallCatalog binds an entire checked batch, not a global mutable directory.
// Rechecking creates a NEW catalog, so category withdrawal/reclassification
// cannot be hidden by an earlier mapping. Empty batches are valid empty scopes.
func NewMallCatalog(candidates []agent.ProductCandidate) (*Catalog, error) {
	if len(candidates) > candidatecontract.MaxCandidates {
		return nil, ErrInvalid
	}
	bindings := make([]mallBinding, 0, len(candidates))
	parents := make(map[string]agent.DemandCategoryEvidence, len(candidates))
	var checkedAt int64
	c := &Catalog{mall: make(map[string]mallBinding, len(candidates)), entries: map[string]CategoryEntry{}}
	for _, candidate := range candidates {
		e := candidate.Evidence
		if !candidatecontract.ValidID(candidate.Id) || !candidatecontract.ValidID(e.ProductID) ||
			!validMallEvidence(e) || c.mall[candidate.Id].SKUID != "" {
			return nil, ErrInvalid
		}
		// Classification belongs to the SPU, not each SKU. A single Mall SQL
		// snapshot cannot legitimately report two category revisions for it.
		if prior, exists := parents[e.ProductID]; exists && prior != e.DemandCategory {
			return nil, ErrInvalid
		}
		if checkedAt != 0 && checkedAt != e.VerifiedAtUnixMs {
			return nil, ErrInvalid
		}
		parents[e.ProductID], checkedAt = e.DemandCategory, e.VerifiedAtUnixMs
		binding := mallBinding{SKUID: candidate.Id, ProductID: e.ProductID, Category: e.DemandCategory, CheckedAt: e.VerifiedAtUnixMs}
		c.mall[candidate.Id] = binding
		bindings = append(bindings, binding)
	}
	slices.SortFunc(bindings, func(a, b mallBinding) int {
		if a.SKUID < b.SKUID {
			return -1
		}
		if a.SKUID > b.SKUID {
			return 1
		}
		return 0
	})
	encoded, err := json.Marshal(bindings)
	if err != nil {
		return nil, ErrInvalid
	}
	c.sha256 = fmt.Sprintf("%x", sha256.Sum256(encoded))
	c.metadata = CatalogMetadata{Version: "mall_demand_mapping_v1", TaxonomyVersion: candidatecontract.DemandTaxonomyVersion,
		Provenance: "mall_managed_classification", AnnotationStatus: "operator_managed",
		DatasetBinding: DatasetBinding{Version: candidatecontract.DemandCategoryContract, SHA256: c.sha256}}
	return c, nil
}

func validMallEvidence(e agent.CandidateEvidence) bool {
	return (e.Source == agent.RetrievalMallKeyword || e.Source == agent.RetrievalMallVector) &&
		e.State == agent.VerificationChecked && e.VerifiedAtUnixMs > 0 &&
		candidatecontract.ValidDemandCategoryFact(e.DemandCategory.Code, e.DemandCategory.TaxonomyVersion, e.DemandCategory.Revision)
}

func (c *Catalog) classifyMall(candidate agent.ProductCandidate, out Classification) Classification {
	if !validMallEvidence(candidate.Evidence) {
		out.Reason = "not_checked_mall_source"
		return out
	}
	binding, ok := c.mall[candidate.Id]
	if !ok {
		out.Reason = "unmapped_sku"
		return out
	}
	if candidate.Evidence.ProductID != binding.ProductID {
		out.Reason = "identity_mismatch"
		return out
	}
	if candidate.Evidence.DemandCategory != binding.Category || candidate.Evidence.VerifiedAtUnixMs != binding.CheckedAt {
		out.Reason = "changed_category_evidence"
		return out
	}
	out.Category, out.Reason = Category(binding.Category.Code), "mapped_mall"
	if out.Category == Unknown {
		out.Reason = "unclassified_mall"
	}
	return out
}
