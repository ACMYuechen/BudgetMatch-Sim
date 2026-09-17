package product_vectors

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var (
	ErrProfileMismatch = errors.New("product_vectors: index profile mismatch; explicit migration required")
	ErrUnboundIndex    = errors.New("product_vectors: existing vectors have no model profile; explicit migration required")
)

// Profile contains only a digest, never credentials or the raw provider URL.
// It binds model/provider/endpoint/dimension identity to the whole index.
type Profile struct {
	Fingerprint string
	Dimensions  int
}

func (p Profile) Validate() error {
	digest, err := hex.DecodeString(p.Fingerprint)
	if err != nil || len(digest) != 32 || p.Dimensions <= 0 {
		return fmt.Errorf("product_vectors: invalid index profile")
	}
	return nil
}

func (m *defaultProductVectorsModel) IndexProfile() Profile { return m.profile }

func (m *defaultProductVectorsModel) checkProfile(ctx context.Context) error {
	var stored Profile
	result := m.conn.WithContext(ctx).Raw("SELECT fingerprint, dimensions FROM product_vector_profile WHERE id = 1").Scan(&stored)
	if result.Error != nil {
		return fmt.Errorf("product_vectors: read index profile: %w", result.Error)
	}
	if result.RowsAffected != 1 || stored != m.profile {
		return ErrProfileMismatch
	}
	return nil
}

// Initialize bootstraps an empty index or verifies an existing binding. It never
// drops/rebuilds vector data or silently adopts legacy vectors of unknown origin.
// Schema creation and binding share the sync lock and one short transaction.
func (m *defaultProductVectorsModel) Initialize(ctx context.Context) error {
	if err := m.profile.Validate(); err != nil {
		return err
	}
	return m.withSession(ctx, func(session *defaultProductVectorsModel) error {
		return session.conn.Transaction(func(tx *gorm.DB) error {
			bound := &defaultProductVectorsModel{conn: tx, profile: m.profile, ownedSession: true}
			return bound.initializeSchema(ctx)
		})
	})
}

func (m *defaultProductVectorsModel) initializeSchema(ctx context.Context) error {
	if err := m.conn.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return err
	}
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS product_vectors (
		sku_id VARCHAR(64) PRIMARY KEY,
		product_id VARCHAR(64) NOT NULL,
		content TEXT NOT NULL,
		metadata JSONB NOT NULL DEFAULT '{}',
		embedding vector(%d) NOT NULL,
		content_hash VARCHAR(64) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`, m.profile.Dimensions)
	if err := m.conn.Exec(ddl).Error; err != nil {
		return err
	}
	dim, err := m.embeddingDim()
	if err != nil {
		return err
	}
	if dim != m.profile.Dimensions {
		return ErrProfileMismatch
	}
	if err := m.conn.Exec(`CREATE TABLE IF NOT EXISTS product_vector_profile (
		id SMALLINT PRIMARY KEY CHECK (id = 1),
		fingerprint VARCHAR(64) NOT NULL,
		dimensions INTEGER NOT NULL CHECK (dimensions > 0)
	)`).Error; err != nil {
		return err
	}
	var stored Profile
	result := m.conn.Raw("SELECT fingerprint, dimensions FROM product_vector_profile WHERE id = 1").Scan(&stored)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var populated bool
		if err := m.conn.Raw("SELECT EXISTS (SELECT 1 FROM product_vectors) AS populated").Scan(&populated).Error; err != nil {
			return err
		}
		if populated {
			return ErrUnboundIndex
		}
		if err := m.conn.Exec("INSERT INTO product_vector_profile (id, fingerprint, dimensions) VALUES (1, ?, ?)",
			m.profile.Fingerprint, m.profile.Dimensions).Error; err != nil {
			return err
		}
	} else if stored != m.profile {
		return ErrProfileMismatch
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.ensureIndex()
}
