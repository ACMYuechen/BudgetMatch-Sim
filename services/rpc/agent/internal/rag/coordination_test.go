package rag

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/model/product_vectors"
	"github.com/stretchr/testify/require"
)

type syncCoordinatorFunc func(context.Context, string, func(product_vectors.SyncStore) error) error

func (f syncCoordinatorFunc) WithSync(ctx context.Context, fingerprint string, fn func(product_vectors.SyncStore) error) error {
	return f(ctx, fingerprint, fn)
}

func TestPipelineCoordinatorAdmissionPrecedesCatalogAndEmbedding(t *testing.T) {
	for _, failure := range []error{ErrSyncBusy, product_vectors.ErrProfileMismatch, errors.New("database unavailable")} {
		loads := 0
		loader := &fakeLoader{load: func(context.Context) (CatalogScan, error) { loads++; return CatalogScan{Complete: true}, nil }}
		idx := &fakeIndexer{}
		p, err := NewPipeline(loader, nil, idx, syncCoordinatorFunc(func(_ context.Context, fp string, _ func(product_vectors.SyncStore) error) error {
			require.Equal(t, "expected-profile", fp)
			return failure
		}), "expected-profile")
		require.NoError(t, err)
		stats, err := p.Sync(context.Background())
		require.ErrorIs(t, err, failure)
		require.Equal(t, SyncStats{}, stats)
		require.Zero(t, loads)
		require.Zero(t, idx.calls)
	}
}

func TestPipelineUsesOnlyOwnedStoreAndHonorsAdmissionCancellation(t *testing.T) {
	for _, cancelOnAdmission := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		model := &fakeVectorModel{hashes: map[string]string{"stale": "old"}, pruned: 1}
		loads := 0
		p, err := NewPipeline(&fakeLoader{load: func(context.Context) (CatalogScan, error) {
			loads++
			return CatalogScan{Complete: true}, nil
		}}, nil, &fakeIndexer{}, syncCoordinatorFunc(func(_ context.Context, _ string, fn func(product_vectors.SyncStore) error) error {
			if cancelOnAdmission {
				cancel()
			}
			return fn(model) // Coordinator exposes no ListHashes/PublishSync itself.
		}), "test")
		require.NoError(t, err)
		stats, err := p.Sync(ctx)
		cancel()
		if cancelOnAdmission {
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, loads)
			require.Zero(t, model.publishCalls)
		} else {
			require.NoError(t, err)
			require.EqualValues(t, 1, stats.Pruned)
			require.Equal(t, 1, model.publishCalls)
			require.Empty(t, model.hashes)
		}
	}
}

func TestSeparatePipelinesShareCoordinatorBeforeScanning(t *testing.T) {
	var lease sync.Mutex
	model := &fakeVectorModel{}
	coordinator := syncCoordinatorFunc(func(_ context.Context, _ string, fn func(product_vectors.SyncStore) error) error {
		if !lease.TryLock() {
			return ErrSyncBusy
		}
		defer lease.Unlock()
		return fn(model)
	})
	entered, done := make(chan struct{}), make(chan error, 1)
	owner, err := NewPipeline(&fakeLoader{load: func(ctx context.Context) (CatalogScan, error) {
		close(entered)
		<-ctx.Done()
		return CatalogScan{}, ctx.Err()
	}}, nil, &fakeIndexer{}, coordinator, "test")
	require.NoError(t, err)
	var scans atomic.Int32
	contender, err := NewPipeline(&fakeLoader{load: func(context.Context) (CatalogScan, error) {
		scans.Add(1)
		return CatalogScan{Complete: true}, nil
	}}, nil, &fakeIndexer{}, coordinator, "test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _, err := owner.Sync(ctx); done <- err }()
	awaitSignal(t, entered)
	_, err = contender.Sync(context.Background())
	require.ErrorIs(t, err, ErrSyncBusy)
	require.Zero(t, scans.Load(), "a rejected writer must not precompute a stale snapshot")
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("owner did not exit")
	}
	_, err = contender.Sync(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, scans.Load())
	require.Equal(t, 1, model.publishCalls)
}

type busyVectorModel struct{ fakeVectorModel }

func (*busyVectorModel) WithSync(context.Context, string, func(product_vectors.SyncStore) error) error {
	return ErrSyncBusy
}

func TestStandaloneIndexerCannotBypassCoordinator(t *testing.T) {
	calls := 0
	idx := NewIndexer(&Store{model: &busyVectorModel{}, dim: 3,
		embedder: embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) { calls++; return testVectors(texts), nil }),
	})
	_, err := idx.Store(context.Background(), indexDocuments(1))
	require.ErrorIs(t, err, ErrSyncBusy)
	require.Zero(t, calls)
}
