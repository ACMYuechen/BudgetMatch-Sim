package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"sync"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
)

// Pipeline 编排商品向量的离线同步：完整加载 → 校验 → hash 比对 → 准备向量 → 事务发布。
//
// Transformer 位保留以对齐 Eino 的 Loader/Transformer/Indexer/Retriever 四件套抽象；
// 商品文档短且结构化，默认传 nil；转换必须保留一 SKU 一文档及商品身份。
type Pipeline struct {
	loader      CatalogLoader
	transformer document.Transformer
	indexer     RowPreparer
	model       SyncModel
	fingerprint string     // 模型身份指纹，真实切换受模型绑定保护，不自动重建
	running     sync.Mutex // Only guards this Pipeline; not a distributed lock.
}

var ErrSyncBusy = product_vectors.ErrSyncBusy

type RowPreparer interface {
	Prepare(context.Context, []*schema.Document, ...indexer.Option) ([]product_vectors.ProductVectors, error)
}

type SyncModel interface {
	WithSync(context.Context, string, func(product_vectors.SyncStore) error) error
}

// SyncStats 是一次同步的统计结果。
type SyncStats struct {
	Loaded    int   // Loaded 数据源加载的文档总数
	Indexed   int   // Indexed 新增或内容变更、经过重嵌入入库的文档数
	Refreshed int   // Refreshed 内容未变、仅刷新业务快照的文档数
	Pruned    int64 // Pruned 因下架/删除而清理的行数
}

// NewPipeline 组装同步流水线。transformer 可为 nil。
func NewPipeline(loader CatalogLoader, transformer document.Transformer, idx RowPreparer,
	model SyncModel, fingerprint string) (*Pipeline, error) {
	if loader == nil || idx == nil || model == nil || strings.TrimSpace(fingerprint) == "" {
		return nil, fmt.Errorf("rag: catalog loader, row preparer, model and fingerprint are required")
	}
	return &Pipeline{
		loader:      loader,
		transformer: transformer,
		indexer:     idx,
		model:       model,
		fingerprint: fingerprint,
	}, nil
}

// Sync 执行一次全量对比同步。
func (p *Pipeline) Sync(ctx context.Context) (SyncStats, error) {
	var stats SyncStats
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if !p.running.TryLock() {
		return stats, ErrSyncBusy
	}
	defer p.running.Unlock()
	err := p.model.WithSync(ctx, p.fingerprint, func(model product_vectors.SyncStore) error {
		var err error
		stats, err = p.syncOwned(ctx, model)
		return err
	})
	return stats, err
}

// The coordinator must own the index before the first source read, not merely
// before publication. Use only its connection-bound store throughout this run.
func (p *Pipeline) syncOwned(ctx context.Context, model product_vectors.SyncStore) (SyncStats, error) {
	var stats SyncStats
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	scan, err := p.loader.LoadCatalog(ctx)
	if err != nil {
		return stats, err
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if !scan.Complete {
		return stats, fmt.Errorf("rag: catalog scan did not complete")
	}
	docs := scan.Documents
	identities, err := validateCatalogDocuments(docs)
	if err != nil {
		return stats, err
	}
	if p.transformer != nil {
		if docs, err = p.transformer.Transform(ctx, docs); err != nil {
			return stats, fmt.Errorf("rag: transform documents: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		transformed, err := validateCatalogDocuments(docs)
		if err != nil {
			return stats, err
		}
		if len(transformed) != len(identities) {
			return stats, fmt.Errorf("rag: transformer changed catalog coverage")
		}
		for id, meta := range transformed {
			original, ok := identities[id]
			if !ok || meta.ProductId != original.ProductId {
				return stats, fmt.Errorf("rag: transformer changed catalog identity")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	stats.Loaded = len(docs)

	existing, err := model.ListHashes(ctx)
	if err != nil {
		return stats, err
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}

	var changed []*schema.Document
	batch := product_vectors.SyncBatch{Complete: scan.Complete, KeepIDs: make([]string, 0, len(docs))}
	expected := make(map[string]product_vectors.ProductVectors)
	for _, doc := range docs {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		hash := p.contentHash(doc.Content)
		batch.KeepIDs = append(batch.KeepIDs, doc.ID)
		meta, _ := CandidateFromDocument(doc) // Whole-catalog validation already succeeded.
		metadata, err := candidateJSON(meta)
		if err != nil {
			return stats, err
		}

		if existing[doc.ID] == hash {
			batch.MetadataUpdates = append(batch.MetadataUpdates, product_vectors.MetadataUpdate{SkuId: doc.ID, Metadata: metadata})
			continue
		}
		// Do not mutate documents owned by the source or reusable test fixtures.
		copy := *doc
		copy.MetaData = maps.Clone(doc.MetaData)
		copy.MetaData[metaContentHash] = hash
		changed = append(changed, &copy)
		expected[doc.ID] = product_vectors.ProductVectors{SkuId: doc.ID, ProductId: meta.ProductId,
			Content: doc.Content, Metadata: metadata, ContentHash: hash}
	}

	if len(changed) > 0 {
		rows, err := p.indexer.Prepare(ctx, changed)
		if err != nil {
			return stats, err
		}
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if len(rows) != len(expected) {
			return stats, fmt.Errorf("rag: incomplete prepared index rows")
		}
		for _, row := range rows {
			want, ok := expected[row.SkuId]
			if !ok || row.ProductId != want.ProductId || row.Content != want.Content || row.Metadata != want.Metadata || row.ContentHash != want.ContentHash {
				return stats, fmt.Errorf("rag: prepared row does not match catalog")
			}
			delete(expected, row.SkuId)
		}
		batch.Upserts = rows
	}

	if err := batch.Validate(); err != nil {
		return stats, err
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	pruned, err := model.PublishSync(ctx, batch)
	if err != nil {
		return stats, err
	}
	// Only committed writes count as indexed/refreshed/pruned. Cancellation after
	// commit cannot undo a published batch and must not relabel it as uncommitted.
	stats.Indexed = len(batch.Upserts)
	stats.Refreshed = len(batch.MetadataUpdates)
	stats.Pruned = pruned
	return stats, nil
}

// contentHash includes the bound model profile; vectors cannot be reused across
// profiles. Production model changes require explicit index migration first.
func (p *Pipeline) contentHash(content string) string {
	sum := sha256.Sum256([]byte(p.fingerprint + "\x00" + content))
	return hex.EncodeToString(sum[:])
}
