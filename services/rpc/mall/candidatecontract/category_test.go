package candidatecontract

import "testing"

func TestDemandCategoryContractRejectsImplicitOrForeignEvidence(t *testing.T) {
	for _, code := range []string{"unknown", "keyboard", "mouse", "lighting", "stationery", "monitor", "headphones", "tablet", "phone", "computer", "accessories"} {
		if !ValidDemandCategoryFact(code, DemandTaxonomyVersion, 1) {
			t.Fatal(code)
		}
	}
	for _, tc := range []struct {
		code, version string
		revision      int64
	}{
		{"", DemandTaxonomyVersion, 1}, {"quiet", DemandTaxonomyVersion, 1}, {"Keyboard", DemandTaxonomyVersion, 1},
		{"keyboard", "demo_taxonomy_v1", 1}, {"keyboard", "", 1}, {"keyboard", DemandTaxonomyVersion, 0}, {"unknown", DemandTaxonomyVersion, -1},
	} {
		if ValidDemandCategoryFact(tc.code, tc.version, tc.revision) {
			t.Fatalf("accepted invalid fact: %+v", tc)
		}
	}
	if !ValidDemandCategoryFact("unknown", DemandTaxonomyVersion, 0) {
		t.Fatal("explicit unknown must be supported")
	}
}
