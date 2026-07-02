// Package product_vectors 提供商品向量表的 GORM 模型。
//
// 该表是 mall 商品数据的派生缓存（embedding + 业务快照），可随时安全重建，
// 重建后由 RAG 同步器自动回填。embedding 列的维度由运行时配置决定，
// GORM tag 无法表达 vector(N)，DDL 手写在 CreateTable 中，不走 AutoMigrate。
package product_vectors

import (
	"context"
	"fmt"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProductVectors 是商品向量表的 GORM 模型，一行对应一个 mall SKU。
type ProductVectors struct {
	SkuId       string          `gorm:"column:sku_id;type:varchar(64);primaryKey"`      // SkuId mall SKU ID，即候选商品 ID
	ProductId   string          `gorm:"column:product_id;type:varchar(64);not null"`    // ProductId 所属商品 ID
	Content     string          `gorm:"column:content;not null"`                        // Content 参与 embedding 的语义文本（不含价格库存）
	Metadata    string          `gorm:"column:metadata;type:jsonb;not null;default:{}"` // Metadata 业务快照 JSON（名称/分类/价格/库存等）
	Embedding   pgvector.Vector `gorm:"column:embedding"`                               // Embedding 向量，维度见 CreateTable
	ContentHash string          `gorm:"column:content_hash;type:varchar(64);not null"`  // ContentHash sha256(模型+维度+文本)，增量同步指纹
	CreatedAt   time.Time       `gorm:"column:created_at"`                              // CreatedAt 创建时间
	UpdatedAt   time.Time       `gorm:"column:updated_at"`                              // UpdatedAt 更新时间
}

// TableName 返回表名。
func (ProductVectors) TableName() string {
	return "product_vectors"
}

// ScoredProductVector 是带相似度分数的检索结果。
type ScoredProductVector struct {
	ProductVectors
	Score float64 `gorm:"column:score"` // Score 余弦相似度，1 为完全相同
}

var _ ProductVectorsModel = (*defaultProductVectorsModel)(nil)

type (
	// ProductVectorsModel 商品向量表操作接口。
	ProductVectorsModel interface {
		// CreateTable 幂等建表（含 vector 扩展与 HNSW 索引）；表已存在但维度与 dim 不符时重建。
		CreateTable(dim int) error
		// Upsert 按 sku_id 批量插入或更新向量行。
		Upsert(ctx context.Context, rows []ProductVectors) error
		// UpdateMetadata 仅刷新业务快照（价格/库存等），用于文本未变的轻量同步。
		UpdateMetadata(ctx context.Context, skuID string, metadata string) error
		// ListHashes 返回全表 sku_id -> content_hash，用于增量比对。
		ListHashes(ctx context.Context) (map[string]string, error)
		// DeleteNotIn 删除不在保留列表中的行（下架/删除的商品），返回删除行数。
		DeleteNotIn(ctx context.Context, keepSkuIDs []string) (int64, error)
		// SearchByVector 余弦相似度检索 topK 条，结果按相似度降序。
		SearchByVector(ctx context.Context, vec []float32, topK int) ([]ScoredProductVector, error)
	}

	defaultProductVectorsModel struct {
		conn *gorm.DB
	}
)

// NewProductVectorsModel 创建商品向量表 model。
func NewProductVectorsModel(conn *gorm.DB) ProductVectorsModel {
	return &defaultProductVectorsModel{conn: conn}
}

// CreateTable 幂等建表。向量表是派生数据：检测到维度与配置不一致时直接删表重建，
// 由同步器全量回填（有重嵌入的 token 成本，日志醒目提示）。
func (m *defaultProductVectorsModel) CreateTable(dim int) error {
	if dim <= 0 {
		return fmt.Errorf("product_vectors: invalid embedding dimension %d", dim)
	}
	if err := m.conn.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return fmt.Errorf("product_vectors: create pgvector extension: %w", err)
	}

	if m.conn.Migrator().HasTable(&ProductVectors{}) {
		current, err := m.embeddingDim()
		if err != nil {
			return err
		}
		if current == dim {
			return m.ensureIndex()
		}
		logx.Sloww("product_vectors dimension changed, dropping table for full re-embedding (token cost!)",
			logx.Field("current_dim", current), logx.Field("configured_dim", dim))
		if err := m.conn.Migrator().DropTable(&ProductVectors{}); err != nil {
			return fmt.Errorf("product_vectors: drop table for dim change: %w", err)
		}
	}

	// dim 是受配置约束的整数，Sprintf 拼接无注入风险。
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS product_vectors (
		sku_id       VARCHAR(64) PRIMARY KEY,
		product_id   VARCHAR(64) NOT NULL,
		content      TEXT        NOT NULL,
		metadata     JSONB       NOT NULL DEFAULT '{}',
		embedding    vector(%d)  NOT NULL,
		content_hash VARCHAR(64) NOT NULL,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
	)`, dim)
	if err := m.conn.Exec(ddl).Error; err != nil {
		return fmt.Errorf("product_vectors: create table: %w", err)
	}
	return m.ensureIndex()
}

// ensureIndex 幂等创建 HNSW 余弦索引。
// 选 HNSW 而非 ivfflat：空表即可建且无需训练数据，小数据量下召回更好。
func (m *defaultProductVectorsModel) ensureIndex() error {
	err := m.conn.Exec(`CREATE INDEX IF NOT EXISTS idx_product_vectors_embedding
		ON product_vectors USING hnsw (embedding vector_cosine_ops)`).Error
	if err != nil {
		return fmt.Errorf("product_vectors: create hnsw index: %w", err)
	}
	return nil
}

// embeddingDim 读取现有表 embedding 列的维度（pgvector 的 atttypmod 即维度）。
func (m *defaultProductVectorsModel) embeddingDim() (int, error) {
	var dim int
	err := m.conn.Raw(`SELECT atttypmod FROM pg_attribute
		WHERE attrelid = 'product_vectors'::regclass AND attname = 'embedding'`).Scan(&dim).Error
	if err != nil {
		return 0, fmt.Errorf("product_vectors: read embedding dimension: %w", err)
	}
	return dim, nil
}

// Upsert 按 sku_id 批量插入或更新。
func (m *defaultProductVectorsModel) Upsert(ctx context.Context, rows []ProductVectors) error {
	if len(rows) == 0 {
		return nil
	}
	err := m.conn.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "sku_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"product_id", "content", "metadata", "embedding", "content_hash", "updated_at"}),
	}).Create(&rows).Error
	if err != nil {
		return fmt.Errorf("product_vectors: upsert %d rows: %w", len(rows), err)
	}
	return nil
}

// UpdateMetadata 仅刷新业务快照与更新时间。
func (m *defaultProductVectorsModel) UpdateMetadata(ctx context.Context, skuID string, metadata string) error {
	err := m.conn.WithContext(ctx).Model(&ProductVectors{}).
		Where("sku_id = ?", skuID).
		Updates(map[string]any{"metadata": metadata, "updated_at": time.Now()}).Error
	if err != nil {
		return fmt.Errorf("product_vectors: update metadata for %s: %w", skuID, err)
	}
	return nil
}

// ListHashes 返回全表 sku_id -> content_hash 映射。
func (m *defaultProductVectorsModel) ListHashes(ctx context.Context) (map[string]string, error) {
	var rows []struct {
		SkuId       string
		ContentHash string
	}
	err := m.conn.WithContext(ctx).Model(&ProductVectors{}).
		Select("sku_id", "content_hash").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("product_vectors: list hashes: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.SkuId] = row.ContentHash
	}
	return out, nil
}

// DeleteNotIn 删除保留列表之外的行；保留列表为空表示数据源已清空，同步清空整表。
func (m *defaultProductVectorsModel) DeleteNotIn(ctx context.Context, keepSkuIDs []string) (int64, error) {
	session := m.conn.WithContext(ctx)
	var result *gorm.DB
	if len(keepSkuIDs) == 0 {
		result = session.Where("1 = 1").Delete(&ProductVectors{})
	} else {
		result = session.Where("sku_id NOT IN ?", keepSkuIDs).Delete(&ProductVectors{})
	}
	if result.Error != nil {
		return 0, fmt.Errorf("product_vectors: prune stale rows: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// SearchByVector 余弦相似度检索。`<=>` 是 pgvector 的余弦距离操作符，分数 = 1 - 距离。
// 不回读 embedding 列（体积大且调用方不需要）。
func (m *defaultProductVectorsModel) SearchByVector(ctx context.Context, vec []float32, topK int) ([]ScoredProductVector, error) {
	if topK <= 0 {
		return nil, nil
	}
	query := pgvector.NewVector(vec)
	var rows []ScoredProductVector
	err := m.conn.WithContext(ctx).Raw(`SELECT sku_id, product_id, content, metadata, content_hash,
			1 - (embedding <=> ?) AS score
		FROM product_vectors
		ORDER BY embedding <=> ?
		LIMIT ?`, query, query, topK).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("product_vectors: vector search: %w", err)
	}
	return rows, nil
}
