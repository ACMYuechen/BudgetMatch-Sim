package rag

import (
	"context"
	"fmt"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/cloudwego/eino/components/embedding"
)

// Store 聚合向量表 model、embedder 与配置，供 Indexer/Retriever 共享。
type Store struct {
	model       product_vectors.ProductVectorsModel
	embedder    embedding.Embedder
	cfg         Config
	dim         int
	fingerprint string
}

// NewStore 创建向量存储并确保表结构就绪（含维度校验）。
func NewStore(ctx context.Context, model product_vectors.ProductVectorsModel, embedder embedding.Embedder, cfg Config) (*Store, error) {
	if model == nil {
		return nil, fmt.Errorf("rag: product vectors model is required")
	}
	if embedder == nil {
		return nil, fmt.Errorf("rag: embedder is required")
	}
	if err := model.Initialize(ctx); err != nil {
		return nil, err
	}
	profile := model.IndexProfile()
	return &Store{
		model:       model,
		embedder:    embedder,
		cfg:         cfg.Normalize(),
		dim:         profile.Dimensions,
		fingerprint: profile.Fingerprint,
	}, nil
}

// Fingerprint is the immutable model profile used by both the index and hash salt.
func (s *Store) Fingerprint() string {
	return s.fingerprint
}
