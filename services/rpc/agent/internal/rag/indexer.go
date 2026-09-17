package rag

import (
	"context"
	"fmt"
	"math"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/pgvector/pgvector-go"
)

// pgVector 把 embedding 返回的 float64 向量转为 pgvector 存储类型。
func pgVector(vec []float64) pgvector.Vector {
	return pgvector.NewVector(toFloat32(vec))
}

// embedBatchSize 是单次 embedding 请求的最大文本数，避免超过服务端批量上限。
const embedBatchSize = 64

// Indexer 实现 eino indexer.Indexer：批量 embedding 商品文档并 upsert 进向量表。
type Indexer struct {
	store *Store
}

// 确保 Indexer 实现 indexer.Indexer。
var _ indexer.Indexer = (*Indexer)(nil)

// NewIndexer 创建基于 pgvector 的索引器。
func NewIndexer(store *Store) *Indexer {
	return &Indexer{store: store}
}

// Store 索引文档，返回成功入库的文档 ID。
func (i *Indexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) (ids []string, err error) {
	ctx = callbacks.EnsureRunInfo(ctx, i.GetType(), components.ComponentOfIndexer)
	ctx = callbacks.OnStart(ctx, &indexer.CallbackInput{Docs: docs})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	rows, err := i.Prepare(ctx, docs, opts...)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = i.store.model.Upsert(ctx, rows); err != nil {
		return nil, err
	}

	ids = make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SkuId)
	}
	callbacks.OnEnd(ctx, &indexer.CallbackOutput{IDs: ids})
	return ids, nil
}

// Prepare embeds and validates all changed documents without writing the live
// index. Pipeline publishes these rows together with metadata and pruning in one
// short DB transaction; no database transaction spans external embedding calls.
// It does not emit a Store-success callback before the rows are actually saved.
func (i *Indexer) Prepare(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]product_vectors.ProductVectors, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := validateCatalogDocuments(docs); err != nil {
		return nil, err
	}
	co := indexer.GetCommonOptions(&indexer.Options{Embedding: i.store.embedder}, opts...)
	if co.Embedding == nil {
		return nil, fmt.Errorf("rag: embedder is required")
	}
	rows := make([]product_vectors.ProductVectors, 0, len(docs))
	for batchStart := 0; batchStart < len(docs); batchStart += embedBatchSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch := docs[batchStart:min(batchStart+embedBatchSize, len(docs))]

		texts := make([]string, 0, len(batch))
		for _, doc := range batch {
			texts = append(texts, doc.Content)
		}
		vectors, embedErr := co.Embedding.EmbedStrings(ctx, texts)
		if embedErr != nil {
			return nil, fmt.Errorf("rag: embed %d documents: %w", len(batch), embedErr)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(vectors) != len(batch) {
			return nil, fmt.Errorf("rag: expected %d vectors, got %d", len(batch), len(vectors))
		}

		for j, doc := range batch {
			row, rowErr := i.buildRow(doc, vectors[j])
			if rowErr != nil {
				return nil, rowErr
			}
			rows = append(rows, row)
		}
	}

	return rows, nil
}

// buildRow 把带业务快照的文档转换为向量表行。
func (i *Indexer) buildRow(doc *schema.Document, vec []float64) (product_vectors.ProductVectors, error) {
	if len(vec) != i.store.dim || i.store.dim <= 0 {
		return product_vectors.ProductVectors{}, fmt.Errorf("rag: unexpected embedding dimension")
	}
	nonzero := false
	for _, value := range vec {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > math.MaxFloat32 {
			return product_vectors.ProductVectors{}, fmt.Errorf("rag: invalid embedding value")
		}
		nonzero = nonzero || float32(value) != 0
	}
	if !nonzero {
		return product_vectors.ProductVectors{}, fmt.Errorf("rag: zero embedding cannot support cosine retrieval")
	}
	meta, ok := CandidateFromDocument(doc)
	if !ok {
		return product_vectors.ProductVectors{}, fmt.Errorf("rag: document %s missing candidate metadata", doc.ID)
	}
	metadata, err := candidateJSON(meta)
	if err != nil {
		return product_vectors.ProductVectors{}, err
	}
	hash, _ := doc.MetaData[metaContentHash].(string)
	return product_vectors.ProductVectors{
		SkuId:       doc.ID,
		ProductId:   meta.ProductId,
		Content:     doc.Content,
		Metadata:    metadata,
		Embedding:   pgVector(vec),
		ContentHash: hash,
	}, nil
}

// GetType 返回组件类型标识。
func (i *Indexer) GetType() string {
	return "PgVectorIndexer"
}

// IsCallbacksEnabled 声明组件自带 callbacks 切面。
func (i *Indexer) IsCallbacksEnabled() bool {
	return true
}
