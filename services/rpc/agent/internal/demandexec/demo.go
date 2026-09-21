package demandexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/recommend/beam"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// NewBuiltinDemo uses the existing seven authored mock products. Its explicit
// SKU annotations form a NEW synthetic binding, never relabel archived evals.
// No database, RPC, embedding or model is used.
func NewBuiltinDemo() (*Executor, error) {
	products, err := tools.NewMockProductProvider().SearchProducts(context.Background(), tools.SearchProductsReq{})
	if err != nil {
		return nil, err
	}
	labels := map[string]demand.Category{
		"mock_keyboard_001": demand.Keyboard, "mock_mouse_001": demand.Mouse, "mock_lamp_001": demand.Lighting,
		"mock_monitor_001": demand.Monitor, "mock_headset_001": demand.Headphones,
		"mock_notebook_001": demand.Stationery, "mock_pen_001": demand.Stationery,
	}
	entries := make([]demand.CategoryEntry, 0, len(products))
	if len(products) != len(labels) {
		return nil, agent.ErrUnsafeResult
	}
	for i := range products {
		category, ok := labels[products[i].Id]
		if !ok {
			return nil, agent.ErrUnsafeResult
		}
		products[i].Evidence = agent.CandidateEvidence{Source: agent.RetrievalDemo, State: agent.VerificationDemo, ProductID: "demo-" + products[i].Id}
		entries = append(entries, demand.CategoryEntry{SKUID: products[i].Id, ProductID: products[i].Evidence.ProductID, Category: category})
	}
	data, err := json.Marshal(products)
	if err != nil {
		return nil, err
	}
	binding := demand.DatasetBinding{Version: "builtin_demo_products_v1", SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	encoded, err := json.Marshal(struct {
		demand.CatalogMetadata
		Entries []demand.CategoryEntry `json:"entries"`
	}{demand.CatalogMetadata{Version: "builtin_demo_categories_v1", TaxonomyVersion: demand.TaxonomyVersion,
		Provenance: "synthetic_demo", AnnotationStatus: "pending_human_review", DatasetBinding: binding}, entries})
	if err != nil {
		return nil, err
	}
	catalog, err := demand.LoadDemoCatalog(bytes.NewReader(encoded), binding)
	if err != nil {
		return nil, err
	}
	return New(&demoSource{products: cloneCandidates(products)}, catalog, beam.Config{})
}

type demoSource struct{ products []agent.ProductCandidate }

func (s *demoSource) Search(ctx context.Context, _ agent.Intent) ([]agent.ProductCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A closed seven-product universe, not a semantic relevance claim. Retaining
	// every product avoids keyword text silently removing a required category.
	return cloneCandidates(s.products), nil
}

func (s *demoSource) Recheck(ctx context.Context, requested []agent.ProductCandidate) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	byID := make(map[string]agent.ProductCandidate, len(s.products))
	for _, c := range s.products {
		byID[c.Id] = c
	}
	batch := Batch{CheckedAtMs: time.Now().UnixMilli()}
	for _, original := range requested {
		c, ok := byID[original.Id]
		if !ok || c.PriceCents <= 0 || c.Stock <= 0 {
			batch.Unavailable = append(batch.Unavailable, original.Id)
			continue
		}
		c.Tags = append([]string(nil), c.Tags...)
		c.Evidence.VerifiedAtUnixMs = batch.CheckedAtMs
		batch.Candidates = append(batch.Candidates, c)
	}
	return batch, nil
}
