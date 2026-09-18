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
}

type CandidateReader interface {
	FindActiveCandidates(context.Context, []string) ([]CandidateFacts, error)
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
