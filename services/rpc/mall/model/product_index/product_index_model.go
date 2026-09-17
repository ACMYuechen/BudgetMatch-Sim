// Package product_index provides a read-only, bounded projection for Agent indexing.
package product_index

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

const (
	DefaultPageSize = 100
	MaxPageSize     = 200
	MaxCursorBytes  = 128
)

// Entry deliberately excludes user IDs, internal agent comments and order data.
type Entry struct {
	SkuId          string
	ProductId      string
	ProductName    string
	ProductContent string
	Provider       string
	SkuName        string
	Specs          string
	Price          int64
	Stock          int64
	Sold           int64
}

type Reader interface {
	// ListPage returns at most limit rows; the caller uses one look-ahead row.
	ListPage(ctx context.Context, cursor string, limit int) ([]Entry, error)
}

type Model struct{ conn *gorm.DB }

func NewModel(conn *gorm.DB) *Model { return &Model{conn: conn} }

func ValidCursor(cursor string) bool {
	return len(cursor) <= MaxCursorBytes && utf8.ValidString(cursor) &&
		strings.TrimSpace(cursor) == cursor && !strings.ContainsRune(cursor, '\x00')
}

func (m *Model) ListPage(ctx context.Context, cursor string, limit int) ([]Entry, error) {
	if !ValidCursor(cursor) || limit < 1 || limit > MaxPageSize+1 {
		return nil, fmt.Errorf("invalid product index page bounds")
	}
	var rows []Entry
	err := pageQuery(m.conn.WithContext(ctx), cursor, limit).Find(&rows).Error
	return rows, err
}

func pageQuery(db *gorm.DB, cursor string, limit int) *gorm.DB {
	query := db.Table("product_skus AS s").
		Select(`s.id AS sku_id, p.id AS product_id, p.name AS product_name,
			COALESCE(p.content::text, '') AS product_content, COALESCE(p.providor, '') AS provider,
			s.name AS sku_name, COALESCE(s.specs::text, '') AS specs, s.price, s.stock, s.sold`).
		Joins("JOIN products AS p ON p.id = s.product_id").
		Where("p.status = ? AND s.status = ?", 1, 1).
		Where("p.deleted_at IS NULL AND s.deleted_at IS NULL")
	if cursor != "" {
		query = query.Where(`s.id COLLATE "C" > ?`, cursor)
	}
	// C collation matches the byte ordering used by the Agent's cursor checks.
	// Each page is a live read, not a cross-page transactional snapshot (M3.2).
	return query.Order(`s.id COLLATE "C" ASC`).Limit(limit)
}
