package product_index

import (
	"context"
	"fmt"

	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"gorm.io/gorm"
)

// CandidateFacts is smaller than the indexing projection: no free-text content,
// user IDs, internal comments, provider specs or order data are read.
type CandidateFacts struct {
	SkuId, ProductId, ProductName, SkuName string
	Price, Stock, Sold                     int64
	CategoryCode, CategoryTaxonomy         string
	CategoryRevision                       int64
}

type CandidateReader interface {
	FindActiveCandidates(context.Context, []string) ([]CandidateFacts, error)
}

// Kept separate so legacy candidate checks never require the new table.
type DemandCandidateReader interface {
	FindActiveDemandCandidates(context.Context, []string) ([]CandidateFacts, error)
}

func (m *Model) FindActiveDemandCandidates(ctx context.Context, ids []string) ([]CandidateFacts, error) {
	if !candidatecontract.ValidIDs(ids) {
		return nil, fmt.Errorf("invalid candidate check bounds")
	}
	var rows []CandidateFacts
	err := demandCandidateQuery(m.conn.WithContext(ctx), ids).Find(&rows).Error
	return rows, err
}

func demandCandidateQuery(db *gorm.DB, ids []string) *gorm.DB {
	// Price, stock, parent status and classification share ONE statement snapshot.
	// A LEFT JOIN distinguishes a live but unclassified SKU from an unavailable one.
	return candidateQuery(db, ids).
		Joins("LEFT JOIN product_demand_categories AS dc ON dc.product_id = p.id").
		Select(`s.id AS sku_id, p.id AS product_id, p.name AS product_name, s.name AS sku_name,
			s.price, s.stock, s.sold, COALESCE(dc.category_code, 'unknown') AS category_code,
			COALESCE(dc.taxonomy_version, ?) AS category_taxonomy,
			COALESCE(dc.revision, 0) AS category_revision`, candidatecontract.DemandTaxonomyVersion)
}

func (m *Model) FindActiveCandidates(ctx context.Context, ids []string) ([]CandidateFacts, error) {
	if !candidatecontract.ValidIDs(ids) {
		return nil, fmt.Errorf("invalid candidate check bounds")
	}
	var rows []CandidateFacts
	err := candidateQuery(m.conn.WithContext(ctx), ids).Find(&rows).Error
	return rows, err
}

func candidateQuery(db *gorm.DB, ids []string) *gorm.DB {
	// One statement sees both parent and child in the same PostgreSQL snapshot.
	return activeQuery(db).
		Select("s.id AS sku_id, p.id AS product_id, p.name AS product_name, s.name AS sku_name, s.price, s.stock, s.sold").
		Where("s.id IN ?", ids).Limit(candidatecontract.MaxCandidates)
}
