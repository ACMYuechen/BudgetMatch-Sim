package product_vectors

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

type MetadataUpdate struct {
	SkuId    string
	Metadata string
}

// SyncBatch is a prepared full scan. Complete must be explicit even when empty;
// KeepIDs must exactly equal the disjoint union of upserts and metadata updates.
type SyncBatch struct {
	Complete        bool
	Upserts         []ProductVectors
	MetadataUpdates []MetadataUpdate
	KeepIDs         []string
}

func (batch SyncBatch) Validate() error {
	if !batch.Complete {
		return fmt.Errorf("product_vectors: incomplete synchronization batch")
	}
	keep := make(map[string]bool, len(batch.KeepIDs))
	validID := func(id string) bool {
		return id != "" && len(id) <= 64 && strings.TrimSpace(id) == id && utf8.ValidString(id) && !strings.ContainsRune(id, '\x00')
	}
	for _, id := range batch.KeepIDs {
		if !validID(id) {
			return fmt.Errorf("product_vectors: invalid retained SKU")
		}
		if _, duplicate := keep[id]; duplicate {
			return fmt.Errorf("product_vectors: duplicate retained SKU")
		}
		keep[id] = false
	}
	claim := func(id, metadata string) error {
		seen, exists := keep[id]
		metadata = strings.TrimSpace(metadata)
		if !exists || seen || !strings.HasPrefix(metadata, "{") || !json.Valid([]byte(metadata)) {
			return fmt.Errorf("product_vectors: invalid batch coverage or metadata")
		}
		keep[id] = true
		return nil
	}
	for _, row := range batch.Upserts {
		if !validID(row.ProductId) || strings.TrimSpace(row.Content) == "" || row.ContentHash == "" || len(row.ContentHash) > 64 || len(row.Embedding.Slice()) == 0 {
			return fmt.Errorf("product_vectors: invalid prepared row")
		}
		nonzero := false
		for _, value := range row.Embedding.Slice() {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return fmt.Errorf("product_vectors: invalid prepared embedding")
			}
			nonzero = nonzero || value != 0
		}
		if !nonzero {
			return fmt.Errorf("product_vectors: zero prepared embedding")
		}
		if err := claim(row.SkuId, row.Metadata); err != nil {
			return err
		}
	}
	for _, update := range batch.MetadataUpdates {
		if err := claim(update.SkuId, update.Metadata); err != nil {
			return err
		}
	}
	for _, covered := range keep {
		if !covered {
			return fmt.Errorf("product_vectors: synchronization batch is missing a retained SKU")
		}
	}
	return nil
}

// PublishSync does not change table definitions or embedding dimensions. A SQL
// failure or pre-commit cancellation rolls back upserts, refreshes and pruning
// together. This is atomic publication, not cross-instance scan coordination.
func (m *defaultProductVectorsModel) PublishSync(ctx context.Context, batch SyncBatch) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := batch.Validate(); err != nil {
		return 0, err
	}
	var pruned int64
	err := m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model := &defaultProductVectorsModel{conn: tx}
		// Keep INSERT parameter counts bounded; the enclosing transaction publishes
		// all chunks together. Copy rows so timestamps never mutate the prepared batch.
		now := time.Now().UTC()
		const writeBatchSize = 128
		for start := 0; start < len(batch.Upserts); start += writeBatchSize {
			if err := ctx.Err(); err != nil {
				return err
			}
			rows := append([]ProductVectors(nil), batch.Upserts[start:min(start+writeBatchSize, len(batch.Upserts))]...)
			for i := range rows {
				rows[i].UpdatedAt = now
			}
			if err := model.Upsert(ctx, rows); err != nil {
				return err
			}
		}
		for _, update := range batch.MetadataUpdates {
			if err := ctx.Err(); err != nil {
				return err
			}
			result := tx.Model(&ProductVectors{}).Where("sku_id = ?", update.SkuId).
				Updates(map[string]any{"metadata": update.Metadata, "updated_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("product_vectors: refresh target changed during synchronization")
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		pruned, err = model.DeleteNotIn(ctx, batch.KeepIDs)
		if err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return 0, fmt.Errorf("product_vectors: publish synchronization batch: %w", err)
	}
	return pruned, nil
}
