package candidatecontract

// Separate from the Agent's synthetic taxonomy. The shared codes describe only
// a flat product class, never quality, compatibility or attribute guarantees.
const (
	DemandCategoryContract = "mall_demand_category_v1"
	DemandTaxonomyVersion  = "mall_demand_taxonomy_v1"
)

func ValidDemandCategory(code string) bool {
	switch code {
	case "unknown", "keyboard", "mouse", "lighting", "stationery", "monitor",
		"headphones", "tablet", "phone", "computer", "accessories":
		return true
	}
	return false
}

// Revision zero is reserved for an explicitly unclassified product. A missing
// protobuf field is NOT this fact; clients require the negotiated contract.
func ValidDemandCategoryFact(code, taxonomy string, revision int64) bool {
	return taxonomy == DemandTaxonomyVersion && ValidDemandCategory(code) &&
		(revision > 0 || revision == 0 && code == "unknown")
}
