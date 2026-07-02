package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
)

// Pipeline 编排商品向量的离线同步：加载 → （可选切分）→ hash 比对 → 增量索引 → 清理。
//
// Transformer 位保留以对齐 Eino 的 Loader/Transformer/Indexer/Retriever 四件套抽象；
// 商品文档短且结构化，默认传 nil 跳过切分。
type Pipeline struct {
	loader      document.Loader
	transformer document.Transformer
	indexer     indexer.Indexer
	model       product_vectors.ProductVectorsModel
	fingerprint string // fingerprint 内容指纹盐值（embedding 模型+维度），变更即触发全量重嵌入
}

// SyncStats 是一次同步的统计结果。
type SyncStats struct {
	Loaded    int   // Loaded 数据源加载的文档总数
	Indexed   int   // Indexed 新增或内容变更、经过重嵌入入库的文档数
	Refreshed int   // Refreshed 内容未变、仅刷新业务快照的文档数
	Pruned    int64 // Pruned 因下架/删除而清理的行数
}

// NewPipeline 组装同步流水线。transformer 可为 nil。
func NewPipeline(loader document.Loader, transformer document.Transformer, idx indexer.Indexer,
	model product_vectors.ProductVectorsModel, fingerprint string) (*Pipeline, error) {
	if loader == nil || idx == nil || model == nil {
		return nil, fmt.Errorf("rag: loader, indexer and model are required")
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

	docs, err := p.loader.Load(ctx, document.Source{URI: SourceMallProducts})
	if err != nil {
		return stats, err
	}
	if p.transformer != nil {
		if docs, err = p.transformer.Transform(ctx, docs); err != nil {
			return stats, fmt.Errorf("rag: transform documents: %w", err)
		}
	}
	stats.Loaded = len(docs)

	existing, err := p.model.ListHashes(ctx)
	if err != nil {
		return stats, err
	}

	var changed []*schema.Document
	keep := make([]string, 0, len(docs))
	for _, doc := range docs {
		hash := p.contentHash(doc.Content)
		doc.MetaData[metaContentHash] = hash
		keep = append(keep, doc.ID)

		if existing[doc.ID] == hash {
			// 内容未变：跳过重嵌入，仅刷新价格/库存等业务快照。
			meta, ok := CandidateFromDocument(doc)
			if !ok {
				return stats, fmt.Errorf("rag: document %s missing candidate metadata", doc.ID)
			}
			metadata, err := candidateJSON(meta)
			if err != nil {
				return stats, err
			}
			if err := p.model.UpdateMetadata(ctx, doc.ID, metadata); err != nil {
				return stats, err
			}
			stats.Refreshed++
			continue
		}
		changed = append(changed, doc)
	}

	if len(changed) > 0 {
		ids, err := p.indexer.Store(ctx, changed)
		if err != nil {
			return stats, err
		}
		stats.Indexed = len(ids)
	}

	pruned, err := p.model.DeleteNotIn(ctx, keep)
	if err != nil {
		return stats, err
	}
	stats.Pruned = pruned
	return stats, nil
}

// contentHash 计算内容指纹：把 embedding 模型与维度掺入盐值，
// 模型/维度变更时所有 hash 失效，自然触发全量重嵌入。
func (p *Pipeline) contentHash(content string) string {
	sum := sha256.Sum256([]byte(p.fingerprint + "\x00" + content))
	return hex.EncodeToString(sum[:])
}
