package rag

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type embedderFunc func(context.Context, []string) ([][]float64, error)

func (f embedderFunc) EmbedStrings(ctx context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	return f(ctx, texts)
}

func testVectors(texts []string) [][]float64 {
	vectors := make([][]float64, len(texts))
	for i := range vectors {
		vectors[i] = []float64{1, 0, 0}
	}
	return vectors
}

func indexDocuments(count int) []*schema.Document {
	docs := make([]*schema.Document, count)
	for i := range docs {
		docs[i] = catalogDoc(fmt.Sprintf("s%d", i))
		docs[i].MetaData[metaContentHash] = "prepared-hash"
	}
	return docs
}

func TestIndexerPrepareBatchesWithoutPublishing(t *testing.T) {
	model := &fakeVectorModel{}
	var sizes []int
	embedder := embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) {
		sizes = append(sizes, len(texts))
		return testVectors(texts), nil
	})
	idx := NewIndexer(&Store{model: model, embedder: embedder, dim: 3})
	rows, err := idx.Prepare(context.Background(), indexDocuments(129))
	require.NoError(t, err)
	require.Len(t, rows, 129)
	require.Equal(t, []int{64, 64, 1}, sizes)
	require.Zero(t, model.upsertCalls)
	require.Zero(t, model.publishCalls)
	require.Equal(t, "prepared-hash", rows[128].ContentHash)
	require.Equal(t, []float32{1, 0, 0}, rows[128].Embedding.Slice())
}

func TestIndexerRejectsInvalidEmbeddingBeforeAnyWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vectors [][]float64
	}{
		{"missing", nil},
		{"extra", [][]float64{{1, 0, 0}, {1, 0, 0}}},
		{"dimension", [][]float64{{1, 0}}},
		{"NaN", [][]float64{{math.NaN(), 0, 0}}},
		{"positive infinity", [][]float64{{math.Inf(1), 0, 0}}},
		{"negative infinity", [][]float64{{math.Inf(-1), 0, 0}}},
		{"float32 overflow", [][]float64{{math.MaxFloat64, 0, 0}}},
		{"zero vector", [][]float64{{0, 0, 0}}},
		{"float32 underflow", [][]float64{{math.SmallestNonzeroFloat64, 0, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &fakeVectorModel{}
			embedder := embedderFunc(func(context.Context, []string) ([][]float64, error) { return tc.vectors, nil })
			idx := NewIndexer(&Store{model: model, embedder: embedder, dim: 3})
			ids, err := idx.Store(context.Background(), indexDocuments(1))
			require.Error(t, err)
			require.Nil(t, ids)
			require.Zero(t, model.upsertCalls)
		})
	}
}

func TestIndexerValidatesWholeCatalogBeforeEmbedding(t *testing.T) {
	model := &fakeVectorModel{}
	calls := 0
	embedder := embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) {
		calls++
		return testVectors(texts), nil
	})
	idx := NewIndexer(&Store{model: model, embedder: embedder, dim: 3})
	docs := indexDocuments(65)
	docs[64] = nil
	_, err := idx.Prepare(context.Background(), docs)
	require.Error(t, err)
	require.Zero(t, calls, "a bad later batch must be rejected before any paid embedding")
	require.Zero(t, model.upsertCalls)
}

func TestIndexerSecondBatchFailureDiscardsPreparedRows(t *testing.T) {
	model := &fakeVectorModel{}
	calls := 0
	failure := errors.New("embedding unavailable")
	embedder := embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) {
		calls++
		if calls == 2 {
			return nil, failure
		}
		return testVectors(texts), nil
	})
	idx := NewIndexer(&Store{model: model, embedder: embedder, dim: 3})
	ids, err := idx.Store(context.Background(), indexDocuments(129))
	require.ErrorIs(t, err, failure)
	require.Nil(t, ids)
	require.Equal(t, 2, calls)
	require.Zero(t, model.upsertCalls)
}

func TestIndexerCancellationPreventsWriteOrNextBatch(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(fmt.Sprint("before=", before), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			model := &fakeVectorModel{}
			embedder := embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) {
				calls++
				cancel()
				return testVectors(texts), nil // Deliberately ignore cancellation in the dependency.
			})
			idx := NewIndexer(&Store{model: model, embedder: embedder, dim: 3})
			if before {
				cancel()
			}
			ids, err := idx.Store(ctx, indexDocuments(65))
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, ids)
			require.Zero(t, model.upsertCalls)
			if before {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestIndexerStandaloneStoreRejectsEmbeddingOverride(t *testing.T) {
	model := &fakeVectorModel{}
	defaultCalls, overrideCalls := 0, 0
	defaultEmbedder := embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) {
		defaultCalls++
		return testVectors(texts), nil
	})
	override := embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) {
		overrideCalls++
		return testVectors(texts), nil
	})
	idx := NewIndexer(&Store{model: model, embedder: defaultEmbedder, dim: 3})
	ids, err := idx.Store(context.Background(), indexDocuments(2), indexer.WithEmbedding(override))
	require.ErrorContains(t, err, "embedding overrides")
	require.Nil(t, ids)
	_, err = idx.Prepare(context.Background(), indexDocuments(2), indexer.WithEmbedding(override))
	require.ErrorContains(t, err, "embedding overrides")
	require.Zero(t, overrideCalls)
	require.Zero(t, defaultCalls)
	require.Zero(t, model.upsertCalls)
	ids, err = idx.Store(context.Background(), indexDocuments(2))
	require.NoError(t, err)
	require.Equal(t, []string{"s0", "s1"}, ids)
	require.Equal(t, 1, defaultCalls)
	require.Equal(t, 1, model.upsertCalls)
	model.upsertErr = errors.New("write failed")
	ids, err = idx.Store(context.Background(), indexDocuments(1))
	require.ErrorIs(t, err, model.upsertErr)
	require.Nil(t, ids)
	_, err = idx.Prepare(context.Background(), indexDocuments(1), indexer.WithEmbedding(nil))
	require.Error(t, err)
}
