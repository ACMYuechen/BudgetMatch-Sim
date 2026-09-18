package rag

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// Updating volatile metadata must not consume another embedding call.
func TestSnapshotTimeRefreshDoesNotReembed(t *testing.T) {
	meta := CandidateMetadata{ProductId: "p", Name: "keyboard", PriceCents: 100, Stock: 1, Source: "mall", SnapshotAtUnixMs: 1000}
	loader := &fakeLoader{docs: []*schema.Document{NewCandidateDocument("s", "keyboard text", meta)}}
	idx, model := &fakeIndexer{}, &fakeVectorModel{}
	pipeline, err := NewPipeline(loader, nil, idx, model, "profile")
	require.NoError(t, err)
	_, err = pipeline.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, idx.calls)
	meta.SnapshotAtUnixMs = 2000
	meta.PriceCents = 200
	loader.docs = []*schema.Document{NewCandidateDocument("s", "keyboard text", meta)}
	stats, err := pipeline.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, idx.calls)
	require.Equal(t, 0, stats.Indexed)
	require.Equal(t, 1, stats.Refreshed)
	require.Len(t, model.batch.MetadataUpdates, 1)
	stored, err := candidateFromJSON(model.batch.MetadataUpdates[0].Metadata)
	require.NoError(t, err)
	require.EqualValues(t, 2000, stored.SnapshotAtUnixMs)
	require.EqualValues(t, 200, stored.PriceCents)
	legacy, err := candidateFromJSON(`{"product_id":"p","price_cents":100}`)
	require.NoError(t, err)
	require.Zero(t, legacy.SnapshotAtUnixMs, "legacy time remains unknown, not fabricated from index update time")
}

// fakeLoader 返回预置文档。
type fakeLoader struct {
	docs []*schema.Document
	load func(context.Context) (CatalogScan, error)
}

func (f *fakeLoader) LoadCatalog(ctx context.Context) (CatalogScan, error) {
	if f.load != nil {
		return f.load(ctx)
	}
	return CatalogScan{Documents: f.docs, Complete: true}, nil
}

// fakeIndexer 只准备行，不写入 model。
type fakeIndexer struct {
	stored  []string
	calls   int
	prepare func(context.Context, []*schema.Document) ([]product_vectors.ProductVectors, error)
}

func (f *fakeIndexer) Prepare(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]product_vectors.ProductVectors, error) {
	f.calls++
	if f.prepare != nil {
		return f.prepare(ctx, docs)
	}
	rows := make([]product_vectors.ProductVectors, 0, len(docs))
	idx := NewIndexer(&Store{dim: 3})
	for _, doc := range docs {
		row, err := idx.buildRow(doc, []float64{1, 0, 0})
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
		f.stored = append(f.stored, doc.ID)
	}
	return rows, nil
}

// fakeVectorModel 是 ProductVectorsModel 的内存替身。
type fakeVectorModel struct {
	hashes          map[string]string
	metadataUpdates []string
	keepOnDelete    []string
	pruned          int64
	listCalls       int
	publishCalls    int
	upsertCalls     int
	listErr         error
	publishErr      error
	upsertErr       error
	onList          func()
	onPublish       func()
	batch           product_vectors.SyncBatch
}

func (f *fakeVectorModel) IndexProfile() product_vectors.Profile {
	return product_vectors.Profile{Fingerprint: strings.Repeat("a", 64), Dimensions: 3}
}
func (f *fakeVectorModel) Initialize(context.Context) error { return nil }
func (f *fakeVectorModel) WithSync(ctx context.Context, _ string, fn func(product_vectors.SyncStore) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(f)
}

func (f *fakeVectorModel) Upsert(ctx context.Context, rows []product_vectors.ProductVectors) error {
	f.upsertCalls++
	return f.upsertErr
}

func (f *fakeVectorModel) UpdateMetadata(ctx context.Context, skuId string, metadata string) error {
	f.metadataUpdates = append(f.metadataUpdates, skuId)
	return nil
}

func (f *fakeVectorModel) ListHashes(ctx context.Context) (map[string]string, error) {
	f.listCalls++
	if f.onList != nil {
		f.onList()
	}
	return maps.Clone(f.hashes), f.listErr
}

func (f *fakeVectorModel) PublishSync(ctx context.Context, batch product_vectors.SyncBatch) (int64, error) {
	f.publishCalls++
	if err := batch.Validate(); err != nil {
		return 0, err
	}
	if f.onPublish != nil {
		f.onPublish()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if f.publishErr != nil {
		return 0, f.publishErr
	}
	f.batch = batch
	if f.hashes == nil {
		f.hashes = make(map[string]string)
	}
	for _, row := range batch.Upserts {
		f.hashes[row.SkuId] = row.ContentHash
	}
	for _, update := range batch.MetadataUpdates {
		f.metadataUpdates = append(f.metadataUpdates, update.SkuId)
	}
	f.keepOnDelete = append([]string{}, batch.KeepIDs...)
	keep := make(map[string]bool)
	for _, id := range batch.KeepIDs {
		keep[id] = true
	}
	for id := range f.hashes {
		if !keep[id] {
			delete(f.hashes, id)
		}
	}
	return f.pruned, nil
}

func (f *fakeVectorModel) DeleteNotIn(ctx context.Context, keepSkuIds []string) (int64, error) {
	f.keepOnDelete = keepSkuIds
	return f.pruned, nil
}

func (f *fakeVectorModel) SearchByVector(ctx context.Context, vec []float32, topK int) ([]product_vectors.ScoredProductVector, error) {
	return nil, nil
}

// TestPipelineSyncPartition 验证同步分流：
// 内容未变仅刷快照、新增/变更走索引、快照外的行被清理。
func TestPipelineSyncPartition(t *testing.T) {
	meta := CandidateMetadata{ProductId: "p1", Name: "n", Source: "mall"}
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
	docs := []*schema.Document{NewCandidateDocument("sku-1", "文本", CandidateMetadata{ProductId: "p1"})}

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

func catalogDoc(id string) *schema.Document {
	return NewCandidateDocument(id, "商品 "+id, CandidateMetadata{ProductId: "p1", PriceCents: 100, Stock: 3})
}

type transformerFunc func(context.Context, []*schema.Document) ([]*schema.Document, error)

func (f transformerFunc) Transform(ctx context.Context, docs []*schema.Document, _ ...document.TransformerOption) ([]*schema.Document, error) {
	return f(ctx, docs)
}

func TestPipelineRejectsUnsafeCatalogBeforeExternalWork(t *testing.T) {
	for _, tc := range []struct {
		name string
		scan CatalogScan
	}{
		{"zero scan", CatalogScan{}},
		{"partial scan", CatalogScan{Documents: []*schema.Document{catalogDoc("s1")}}},
		{"nil document", CatalogScan{Complete: true, Documents: []*schema.Document{nil}}},
		{"duplicate SKU", CatalogScan{Complete: true, Documents: []*schema.Document{catalogDoc("s1"), catalogDoc("s1")}}},
		{"empty SKU", CatalogScan{Complete: true, Documents: []*schema.Document{catalogDoc("")}}},
		{"overlong SKU", CatalogScan{Complete: true, Documents: []*schema.Document{catalogDoc(strings.Repeat("s", 65))}}},
		{"padded SKU", CatalogScan{Complete: true, Documents: []*schema.Document{catalogDoc(" s1")}}},
		{"NUL SKU", CatalogScan{Complete: true, Documents: []*schema.Document{catalogDoc("s\x00")}}},
		{"invalid UTF8", CatalogScan{Complete: true, Documents: []*schema.Document{catalogDoc(string([]byte{0xff}))}}},
		{"empty content", CatalogScan{Complete: true, Documents: []*schema.Document{NewCandidateDocument("s1", " ", CandidateMetadata{ProductId: "p1"})}}},
		{"missing metadata", CatalogScan{Complete: true, Documents: []*schema.Document{{ID: "s1", Content: "text"}}}},
		{"invalid product", CatalogScan{Complete: true, Documents: []*schema.Document{NewCandidateDocument("s1", "text", CandidateMetadata{ProductId: ""})}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, model := &fakeIndexer{}, &fakeVectorModel{hashes: map[string]string{"old": "hash"}}
			loader := &fakeLoader{load: func(context.Context) (CatalogScan, error) { return tc.scan, nil }}
			p, err := NewPipeline(loader, nil, idx, model, "model:3")
			require.NoError(t, err)
			_, err = p.Sync(context.Background())
			require.Error(t, err)
			require.Zero(t, model.listCalls)
			require.Zero(t, idx.calls)
			require.Zero(t, model.publishCalls)
			require.Equal(t, map[string]string{"old": "hash"}, model.hashes)
		})
	}
}

func TestPipelineOnlyExplicitEmptyScanCanClear(t *testing.T) {
	model := &fakeVectorModel{hashes: map[string]string{"old": "hash"}, pruned: 1}
	idx := &fakeIndexer{}
	p, err := NewPipeline(&fakeLoader{}, nil, idx, model, "model:3")
	require.NoError(t, err)
	stats, err := p.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, SyncStats{Pruned: 1}, stats)
	require.True(t, model.batch.Complete)
	require.Equal(t, 1, model.publishCalls)
	require.Empty(t, model.hashes)
	require.Zero(t, idx.calls)
}

func TestPipelineTransformerMustPreserveCatalogCoverage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transform transformerFunc
	}{
		{"error", func(context.Context, []*schema.Document) ([]*schema.Document, error) {
			return nil, errors.New("transform failed")
		}},
		{"dropped", func(context.Context, []*schema.Document) ([]*schema.Document, error) { return nil, nil }},
		{"added", func(_ context.Context, docs []*schema.Document) ([]*schema.Document, error) {
			return append(docs, catalogDoc("s2")), nil
		}},
		{"SKU replaced", func(context.Context, []*schema.Document) ([]*schema.Document, error) {
			return []*schema.Document{catalogDoc("s2")}, nil
		}},
		{"product replaced", func(context.Context, []*schema.Document) ([]*schema.Document, error) {
			return []*schema.Document{NewCandidateDocument("s1", "text", CandidateMetadata{ProductId: "p2"})}, nil
		}},
		{"invalid output", func(context.Context, []*schema.Document) ([]*schema.Document, error) {
			return []*schema.Document{nil}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, model := &fakeIndexer{}, &fakeVectorModel{}
			p, err := NewPipeline(&fakeLoader{docs: []*schema.Document{catalogDoc("s1")}}, tc.transform, idx, model, "model:3")
			require.NoError(t, err)
			_, err = p.Sync(context.Background())
			require.Error(t, err)
			require.Zero(t, model.listCalls)
			require.Zero(t, idx.calls)
			require.Zero(t, model.publishCalls)
		})
	}
}

func TestPipelinePriceStockOnlyRefreshesMetadata(t *testing.T) {
	doc := catalogDoc("s1")
	idx, model := &fakeIndexer{}, &fakeVectorModel{}
	p, err := NewPipeline(&fakeLoader{docs: []*schema.Document{doc}}, nil, idx, model, "model:3")
	require.NoError(t, err)
	_, err = p.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, idx.calls)
	require.NotContains(t, doc.MetaData, metaContentHash, "source documents must not be modified")
	meta, _ := CandidateFromDocument(doc)
	meta.PriceCents, meta.Stock, meta.Sold = 250, 9, 17
	p.loader = &fakeLoader{docs: []*schema.Document{NewCandidateDocument(doc.ID, doc.Content, meta)}}
	stats, err := p.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, SyncStats{Loaded: 1, Refreshed: 1}, stats)
	require.Equal(t, 1, idx.calls, "price/stock changes require no new embeddings")
	want, err := candidateJSON(meta)
	require.NoError(t, err)
	require.JSONEq(t, want, model.batch.MetadataUpdates[0].Metadata)
	require.Empty(t, model.batch.Upserts)
	// Repeated scans remain idempotent and do not change content hashes.
	_, err = p.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, idx.calls)
}

func TestPipelinePreparationFailureCannotPartiallyPublish(t *testing.T) {
	for _, name := range []string{"embedding error", "missing rows", "duplicate rows", "wrong SKU", "wrong product", "wrong text", "wrong metadata", "wrong hash", "invalid vector", "publish error", "hash read error"} {
		t.Run(name, func(t *testing.T) {
			docs := []*schema.Document{catalogDoc("same"), catalogDoc("new1"), catalogDoc("new2")}
			model, idx := &fakeVectorModel{}, &fakeIndexer{}
			p, err := NewPipeline(&fakeLoader{docs: docs}, nil, idx, model, "model:3")
			require.NoError(t, err)
			before := map[string]string{"same": p.contentHash(docs[0].Content), "stale": "old hash"}
			model.hashes = maps.Clone(before)
			failure := errors.New("injected failure")
			if name == "publish error" {
				model.publishErr = failure
			}
			if name == "hash read error" {
				model.listErr = failure
			}
			idx.prepare = func(ctx context.Context, changed []*schema.Document) ([]product_vectors.ProductVectors, error) {
				require.Empty(t, model.metadataUpdates, "unchanged rows must wait for embedding success")
				require.Nil(t, model.keepOnDelete)
				require.Zero(t, model.publishCalls)
				rows, err := (&fakeIndexer{}).Prepare(ctx, changed)
				if err != nil {
					return nil, err
				}
				switch name {
				case "embedding error":
					return nil, failure
				case "missing rows":
					return rows[:1], nil
				case "duplicate rows":
					rows[1] = rows[0]
				case "wrong SKU":
					rows[0].SkuId = "unknown"
				case "wrong product":
					rows[0].ProductId = "p2"
				case "wrong text":
					rows[0].Content = "different"
				case "wrong metadata":
					rows[0].Metadata = `{}`
				case "wrong hash":
					rows[0].ContentHash = "different"
				case "invalid vector":
					rows[0].Embedding = pgVector([]float64{0, 0, 0})
				}
				return rows, nil
			}
			stats, err := p.Sync(context.Background())
			require.Error(t, err)
			require.Equal(t, SyncStats{Loaded: 3}, stats, "prepared rows are not committed counters")
			require.Equal(t, before, model.hashes)
			require.Empty(t, model.metadataUpdates)
			require.Nil(t, model.keepOnDelete)
			require.Zero(t, model.upsertCalls)
			if name != "publish error" {
				require.Zero(t, model.publishCalls)
			}
		})
	}
}

func TestPipelineCancellationStopsFollowingStages(t *testing.T) {
	for _, stage := range []string{"before load", "after load", "after transform", "after hashes", "after prepare", "publish boundary"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			idx, model := &fakeIndexer{}, &fakeVectorModel{hashes: map[string]string{"old": "hash"}}
			loadCalls := 0
			loader := &fakeLoader{load: func(context.Context) (CatalogScan, error) {
				loadCalls++
				if stage == "after load" {
					cancel()
				}
				return CatalogScan{Complete: true, Documents: []*schema.Document{catalogDoc("s1")}}, nil
			}}
			transform := transformerFunc(func(_ context.Context, docs []*schema.Document) ([]*schema.Document, error) {
				if stage == "after transform" {
					cancel()
				}
				return docs, nil
			})
			model.onList = func() {
				if stage == "after hashes" {
					cancel()
				}
			}
			model.onPublish = func() {
				if stage == "publish boundary" {
					cancel()
				}
			}
			idx.prepare = func(ctx context.Context, docs []*schema.Document) ([]product_vectors.ProductVectors, error) {
				rows, err := (&fakeIndexer{}).Prepare(ctx, docs)
				if stage == "after prepare" {
					cancel()
				}
				return rows, err
			}
			p, err := NewPipeline(loader, transform, idx, model, "model:3")
			require.NoError(t, err)
			if stage == "before load" {
				cancel()
			}
			stats, err := p.Sync(ctx)
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, stats.Indexed)
			require.Zero(t, stats.Refreshed)
			require.Zero(t, stats.Pruned)
			require.Equal(t, map[string]string{"old": "hash"}, model.hashes)
			if stage == "before load" {
				require.Zero(t, loadCalls)
			}
			if stage == "before load" || stage == "after load" || stage == "after transform" {
				require.Zero(t, model.listCalls)
			}
			if stage != "after prepare" && stage != "publish boundary" {
				require.Zero(t, idx.calls)
			}
			if stage != "publish boundary" {
				require.Zero(t, model.publishCalls)
			}
		})
	}
}

func TestPipelineSingleFlightReleasesLockAfterCancellation(t *testing.T) {
	entered := make(chan struct{})
	loader := &fakeLoader{load: func(ctx context.Context) (CatalogScan, error) {
		close(entered)
		<-ctx.Done()
		return CatalogScan{}, ctx.Err()
	}}
	p, err := NewPipeline(loader, nil, &fakeIndexer{}, &fakeVectorModel{}, "model:3")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := p.Sync(ctx); finished <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("sync did not enter loader")
	}
	_, err = p.Sync(context.Background())
	require.ErrorIs(t, err, ErrSyncBusy)
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("sync did not exit")
	}
	p.loader = &fakeLoader{}
	_, err = p.Sync(context.Background())
	require.NoError(t, err)
}
