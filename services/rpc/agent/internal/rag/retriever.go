package rag

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

// Retriever 实现 eino retriever.Retriever，基于 pgvector 做余弦相似度检索。
//
// 通过 EnsureRunInfo + IsCallbacksEnabled 接入 Eino callbacks 体系，
// 成为"一等"组件：被 ReAct 工具内部调用时，会作为同一次运行的嵌套组件
// 被挂在 Generate 上的日志 handler 自动记录，无需额外接线。
type Retriever struct {
	store *Store
}

// 确保 Retriever 实现 retriever.Retriever。
var _ retriever.Retriever = (*Retriever)(nil)

// NewRetriever 创建基于 pgvector 的检索器。
func NewRetriever(store *Store) *Retriever {
	return &Retriever{store: store}
}

// Retrieve 检索与查询语义相似的商品文档，按相似度降序返回。
func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) (docs []*schema.Document, err error) {
	co := retriever.GetCommonOptions(&retriever.Options{
		TopK:           &r.store.cfg.TopK,
		ScoreThreshold: &r.store.cfg.ScoreThreshold,
		Embedding:      r.store.embedder,
	}, opts...)

	topK := r.store.cfg.TopK
	if co.TopK != nil {
		topK = *co.TopK
	}
	scoreThreshold := r.store.cfg.ScoreThreshold
	if co.ScoreThreshold != nil {
		scoreThreshold = *co.ScoreThreshold
	}

	ctx = callbacks.EnsureRunInfo(ctx, r.GetType(), components.ComponentOfRetriever)
	ctx = callbacks.OnStart(ctx, &retriever.CallbackInput{
		Query:          query,
		TopK:           topK,
		ScoreThreshold: &scoreThreshold,
	})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	vectors, err := co.Embedding.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("rag: embed query: %w", err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("rag: expected 1 query vector, got %d", len(vectors))
	}

	rows, err := r.store.model.SearchByVector(ctx, toFloat32(vectors[0]), topK)
	if err != nil {
		return nil, err
	}

	docs = make([]*schema.Document, 0, len(rows))
	for _, row := range rows {
		if row.Score < scoreThreshold {
			continue
		}
		meta, err := candidateFromJSON(row.Metadata)
		if err != nil {
			return nil, err
		}
		doc := NewCandidateDocument(row.SkuId, row.Content, meta)
		docs = append(docs, doc.WithScore(row.Score))
	}

	callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: docs})
	return docs, nil
}

// GetType 返回组件类型标识。
func (r *Retriever) GetType() string {
	return "PgVectorRetriever"
}

// IsCallbacksEnabled 声明组件自带 callbacks 切面。
func (r *Retriever) IsCallbacksEnabled() bool {
	return true
}

// toFloat32 把 embedding 返回的 float64 向量转为 pgvector 需要的 float32。
func toFloat32(vec []float64) []float32 {
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = float32(v)
	}
	return out
}
