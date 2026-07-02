package rag

import (
	"context"
	"testing"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
)

// fakeLoader 返回预置文档。
type fakeLoader struct {
	docs []*schema.Document
}

func (f *fakeLoader) Load(ctx context.Context, src document.Source, opts ...document.LoaderOption) ([]*schema.Document, error) {
	return f.docs, nil
}

// fakeIndexer 记录被要求索引的文档 ID。
type fakeIndexer struct {
	stored []string
}

func (f *fakeIndexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error) {
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	f.stored = append(f.stored, ids...)
	return ids, nil
}

// fakeVectorModel 是 ProductVectorsModel 的内存替身。
type fakeVectorModel struct {
	hashes          map[string]string
	metadataUpdates []string
	keepOnDelete    []string
	pruned          int64
}

func (f *fakeVectorModel) CreateTable(dim int) error { return nil }

func (f *fakeVectorModel) Upsert(ctx context.Context, rows []product_vectors.ProductVectors) error {
	return nil
}

func (f *fakeVectorModel) UpdateMetadata(ctx context.Context, skuID string, metadata string) error {
	f.metadataUpdates = append(f.metadataUpdates, skuID)
	return nil
}

func (f *fakeVectorModel) ListHashes(ctx context.Context) (map[string]string, error) {
	return f.hashes, nil
}

func (f *fakeVectorModel) DeleteNotIn(ctx context.Context, keepSkuIDs []string) (int64, error) {
	f.keepOnDelete = keepSkuIDs
	return f.pruned, nil
}

func (f *fakeVectorModel) SearchByVector(ctx context.Context, vec []float32, topK int) ([]product_vectors.ScoredProductVector, error) {
	return nil, nil
}

// TestPipelineSyncPartition 验证同步分流：
// 内容未变仅刷快照、新增/变更走索引、快照外的行被清理。
func TestPipelineSyncPartition(t *testing.T) {
	meta := CandidateMetadata{ProductID: "p1", Name: "n", Source: "mall"}
	docs := []*schema.Document{
		NewCandidateDocument("sku-unchanged", "同样的文本", meta),
		NewCandidateDocument("sku-new", "新商品", meta),
		NewCandidateDocument("sku-changed", "改过的文本", meta),
	}

	pipeline, err := NewPipeline(&fakeLoader{docs: docs}, nil, &fakeIndexer{}, &fakeVectorModel{}, "model:4")
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}

	model := &fakeVectorModel{
		hashes: map[string]string{
			"sku-unchanged": pipeline.contentHash("同样的文本"), // hash 一致 → 仅刷快照
			"sku-changed":   pipeline.contentHash("旧文本"),   // hash 不一致 → 重嵌入
			"sku-stale":     "whatever",                    // 不在快照中 → 清理
		},
		pruned: 1,
	}
	idx := &fakeIndexer{}
	pipeline, err = NewPipeline(&fakeLoader{docs: docs}, nil, idx, model, "model:4")
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}

	stats, err := pipeline.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if stats.Loaded != 3 || stats.Indexed != 2 || stats.Refreshed != 1 || stats.Pruned != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if len(idx.stored) != 2 || idx.stored[0] != "sku-new" || idx.stored[1] != "sku-changed" {
		t.Fatalf("unexpected indexed ids: %+v", idx.stored)
	}
	if len(model.metadataUpdates) != 1 || model.metadataUpdates[0] != "sku-unchanged" {
		t.Fatalf("unexpected metadata refreshes: %+v", model.metadataUpdates)
	}
	if len(model.keepOnDelete) != 3 {
		t.Fatalf("expected keep list of all current skus, got %+v", model.keepOnDelete)
	}
}

// TestPipelineFingerprintInvalidatesHashes 验证换 embedding 模型（盐值变化）后旧 hash 全部失效。
func TestPipelineFingerprintInvalidatesHashes(t *testing.T) {
	docs := []*schema.Document{NewCandidateDocument("sku-1", "文本", CandidateMetadata{ProductID: "p1"})}

	oldPipeline, _ := NewPipeline(&fakeLoader{docs: docs}, nil, &fakeIndexer{}, &fakeVectorModel{}, "old-model:1536")
	model := &fakeVectorModel{hashes: map[string]string{"sku-1": oldPipeline.contentHash("文本")}}

	idx := &fakeIndexer{}
	newPipeline, _ := NewPipeline(&fakeLoader{docs: docs}, nil, idx, model, "new-model:1024")
	stats, err := newPipeline.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if stats.Indexed != 1 || stats.Refreshed != 0 {
		t.Fatalf("expected full re-embedding after fingerprint change, got %+v", stats)
	}
}
