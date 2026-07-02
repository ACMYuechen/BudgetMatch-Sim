package rag

import (
	"fmt"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/cloudwego/eino/components/embedding"
)

// Store 聚合向量表 model、embedder 与配置，供 Indexer/Retriever 共享。
type Store struct {
	model    product_vectors.ProductVectorsModel
	embedder embedding.Embedder
	cfg      Config
	dim      int
}

// NewStore 创建向量存储并确保表结构就绪（含维度校验）。
func NewStore(model product_vectors.ProductVectorsModel, embedder embedding.Embedder, cfg Config, dim int) (*Store, error) {
	if model == nil {
		return nil, fmt.Errorf("rag: product vectors model is required")
	}
	if embedder == nil {
		return nil, fmt.Errorf("rag: embedder is required")
	}
	if err := model.CreateTable(dim); err != nil {
		return nil, err
	}
	return &Store{
		model:    model,
		embedder: embedder,
		cfg:      cfg.Normalize(),
		dim:      dim,
	}, nil
}

// Fingerprint 返回内容指纹的盐值（模型+维度），模型或维度变更会使全部 hash 失效并触发重嵌入。
func (s *Store) Fingerprint(embedModel string) string {
	return fmt.Sprintf("%s:%d", embedModel, s.dim)
}
