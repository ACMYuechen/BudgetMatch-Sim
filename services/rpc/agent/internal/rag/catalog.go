package rag

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
)

// CatalogScan carries explicit scan completion; a zero-value/partial result must
// never authorize pruning. The Mall loader sets Complete only after a consistent
// snapshot's commit marker AND clean stream EOF have both been observed.
type CatalogScan struct {
	Documents []*schema.Document
	Complete  bool
}

type CatalogLoader interface {
	LoadCatalog(context.Context) (CatalogScan, error)
}

// LoadCatalog preserves the Eino Loader while exposing the completion contract
// required by the synchronization pipeline, including a legitimate empty scan.
func (l *MallProductLoader) LoadCatalog(ctx context.Context) (CatalogScan, error) {
	docs, err := l.Load(ctx, document.Source{URI: SourceMallProducts})
	if err != nil {
		return CatalogScan{}, err
	}
	return CatalogScan{Documents: docs, Complete: true}, nil
}

func validIndexID(id string) bool {
	return id != "" && len(id) <= 64 && strings.TrimSpace(id) == id &&
		utf8.ValidString(id) && !strings.ContainsRune(id, '\x00')
}

// Validate the entire scan before any embedding or mutation, not just unchanged
// rows. IDs match the existing VARCHAR(64) vector schema; SKU identity is unique.
func validateCatalogDocuments(docs []*schema.Document) (map[string]CandidateMetadata, error) {
	identities := make(map[string]CandidateMetadata, len(docs))
	for _, doc := range docs {
		if doc == nil || !validIndexID(doc.ID) || strings.TrimSpace(doc.Content) == "" {
			return nil, fmt.Errorf("rag: invalid catalog document")
		}
		meta, ok := CandidateFromDocument(doc)
		if !ok || !validIndexID(meta.ProductId) {
			return nil, fmt.Errorf("rag: invalid catalog metadata")
		}
		if _, duplicate := identities[doc.ID]; duplicate {
			return nil, fmt.Errorf("rag: duplicate catalog SKU")
		}
		identities[doc.ID] = meta
	}
	return identities, nil
}
