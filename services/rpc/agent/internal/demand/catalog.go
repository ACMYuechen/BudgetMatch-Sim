package demand

import (
	"crypto/sha256"
	"fmt"
	"io"
	"regexp"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

const MaxCatalogBytes = 128 << 10

type DatasetBinding struct {
	Version string `json:"dataset_version"`
	SHA256  string `json:"dataset_sha256"`
}

type CatalogMetadata struct {
	Version          string `json:"version"`
	TaxonomyVersion  string `json:"taxonomy_version"`
	Provenance       string `json:"provenance"`
	AnnotationStatus string `json:"annotation_status"`
	DatasetBinding
}

type CategoryEntry struct {
	SKUID     string   `json:"sku_id"`
	ProductID string   `json:"product_id"`
	Category  Category `json:"category"`
}

type catalogFile struct {
	CatalogMetadata
	Entries []CategoryEntry `json:"entries"`
}

// Catalog is immutable after loading. No caller can replace entries while
// retaining a different mapping's digest. This first loader is DEMO ONLY.
type Catalog struct {
	metadata CatalogMetadata
	sha256   string
	entries  map[string]CategoryEntry
}

var versionID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,79}$`)
var digestID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func LoadDemoCatalog(r io.Reader, expected DatasetBinding) (*Catalog, error) {
	var f catalogFile
	data, err := decodeBounded(r, MaxCatalogBytes, &f)
	if err != nil || !versionID.MatchString(f.Version) || f.TaxonomyVersion != TaxonomyVersion ||
		f.Provenance != "synthetic_demo" || f.AnnotationStatus != "pending_human_review" ||
		!versionID.MatchString(expected.Version) || !digestID.MatchString(expected.SHA256) ||
		f.DatasetBinding != expected || len(f.Entries) == 0 || len(f.Entries) > agent.MaxCandidateIDs {
		return nil, ErrInvalid
	}
	c := &Catalog{metadata: f.CatalogMetadata, sha256: fmt.Sprintf("%x", sha256.Sum256(data)), entries: make(map[string]CategoryEntry, len(f.Entries))}
	for _, entry := range f.Entries {
		if !candidatecontract.ValidID(entry.SKUID) || !candidatecontract.ValidID(entry.ProductID) ||
			(!knownCategory(entry.Category) && entry.Category != Unknown) || c.entries[entry.SKUID].SKUID != "" {
			return nil, ErrInvalid
		}
		c.entries[entry.SKUID] = entry
	}
	return c, nil
}

func (c *Catalog) Metadata() (CatalogMetadata, string) {
	if c == nil {
		return CatalogMetadata{}, ""
	}
	return c.metadata, c.sha256
}

type Classification struct {
	Category       Category `json:"category"`
	Reason         string   `json:"reason"`
	MappingVersion string   `json:"mapping_version"`
	MappingSHA256  string   `json:"mapping_sha256"`
}

// Classify does not read candidate.Category, Name, Tags or model text. Exact
// SKU + parent identity AND explicit demo provenance are required. Live Mall
// candidates can never acquire demo hard-category evidence from this loader.
func (c *Catalog) Classify(candidate agent.ProductCandidate) Classification {
	out := Classification{Category: Unknown, Reason: "mapping_unavailable"}
	if c == nil || c.sha256 == "" {
		return out
	}
	out.MappingVersion, out.MappingSHA256 = c.metadata.Version, c.sha256
	if candidate.Evidence.Source != agent.RetrievalDemo || candidate.Evidence.State != agent.VerificationDemo {
		out.Reason = "not_demo_source"
		return out
	}
	entry, ok := c.entries[candidate.Id]
	if !ok {
		out.Reason = "unmapped_sku"
		return out
	}
	if candidate.Evidence.ProductID != entry.ProductID {
		out.Reason = "identity_mismatch"
		return out
	}
	out.Category, out.Reason = entry.Category, "mapped_demo"
	if entry.Category == Unknown {
		out.Reason = "unclassified_demo"
	}
	return out
}
