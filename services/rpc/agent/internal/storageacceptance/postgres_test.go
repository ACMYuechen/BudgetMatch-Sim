package storageacceptance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/model/conversation_memory"
	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

func storedRequest(cfg targetConfig) memory.SaveTurnReq {
	return memory.SaveTurnReq{UserId: cfg.RunID + "-user", ConversationId: uuid.NewString(),
		TurnId: "first", Title: "synthetic", Query: "键盘", Summary: "first",
		Intent: memory.IntentState{BudgetCents: 30000, MaxItems: 2}, ResultJSON: []byte(`{"summary":"first"}`)}
}

func testPostgresBoundaries(t *testing.T, cfg targetConfig, s *openedStore) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	store := memory.NewPostgres(s.pg, memory.Conf{})
	req := storedRequest(cfg)
	// All operations must reuse the locked connection: the pool has capacity 1.
	require.NoError(t, store.WithConversationLock(ctx, req.UserId, req.ConversationId, func(locked context.Context) error {
		_, _, err := store.SaveTurn(locked, req)
		return err
	}))
	before, exists, err := store.GetConversation(ctx, req.UserId, req.ConversationId)
	require.NoError(t, err)
	require.True(t, exists)
	// The turn INSERT is valid, but the following conversation UPDATE contains a
	// JSONB NUL rejected by PostgreSQL. No custom triggers/schema faults needed.
	model := conversation_memory.NewConversationMemoryModel(s.pg)
	_, _, err = model.SaveTurn(ctx, conversation_memory.SaveTurnReq{
		UserId: req.UserId, ConversationId: req.ConversationId, TurnId: "retry",
		Title: "synthetic", State: `{"invalid":"\u0000"}`, Intent: `{}`, Result: `{}`, Now: time.Now(),
	})
	require.Error(t, err)
	_, found, err := store.FindTurn(ctx, req.UserId, req.ConversationId, "retry")
	require.NoError(t, err)
	require.False(t, found, "turn INSERT survived failed conversation UPDATE")
	after, _, err := store.GetConversation(ctx, req.UserId, req.ConversationId)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed transaction changed version/state/count")
	req.TurnId = "retry"
	_, turn, err := store.SaveTurn(ctx, req)
	require.NoError(t, err)
	require.EqualValues(t, 2, turn.Sequence, "rolled-back turn consumed sequence")

	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	stats := s.pool.Stats()
	go func() {
		finished <- store.WithConversationLock(ctx, req.UserId, req.ConversationId, func(locked context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-locked.Done():
				return locked.Err()
			}
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("owner failed to acquire the single pool connection")
	}
	wait, cancelWait := context.WithTimeout(ctx, 150*time.Millisecond)
	err = store.WithConversationLock(wait, req.UserId, "different-conversation", func(context.Context) error {
		return errors.New("unexpected connection acquired while pool exhausted")
	})
	cancelWait()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	unblock()
	require.NoError(t, <-finished)
	afterStats := s.pool.Stats()
	require.Zero(t, afterStats.InUse)
	require.Greater(t, afterStats.WaitCount, stats.WaitCount)
	require.Greater(t, afterStats.WaitDuration, stats.WaitDuration)
	require.NoError(t, s.pool.PingContext(ctx))
	t.Logf("pool_capacity=1 wait_count_delta=%d wait_ns_delta=%d in_use=%d",
		afterStats.WaitCount-stats.WaitCount, afterStats.WaitDuration-stats.WaitDuration, afterStats.InUse)
}

func testTieredCacheFailure(t *testing.T, cfg targetConfig, s *openedStore) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	req := storedRequest(cfg)
	first, _, err := s.store.SaveTurn(ctx, req)
	require.NoError(t, err)
	_, err = s.store.History(ctx, req.UserId, req.ConversationId, 0)
	require.NoError(t, err)
	cache := memory.NewRedis(s.rdb, memory.Conf{})
	_, hit, err := cache.LoadSnapshot(ctx, req.UserId, req.ConversationId, first.Version, 1)
	require.NoError(t, err)
	require.True(t, hit)
	// Close ONLY a newly created writer client, not Redis or the healthy reader.
	brokenClient, err := cfg.openRedis(ctx)
	require.NoError(t, err)
	require.NoError(t, brokenClient.Close())
	brokenWriter := memory.NewTiered(memory.NewPostgres(s.pg, memory.Conf{}), memory.NewRedis(brokenClient, memory.Conf{}), memory.Conf{})
	req.TurnId, req.Summary, req.ResultJSON = "second", "second", []byte(`{"summary":"second"}`)
	second, _, err := brokenWriter.SaveTurn(ctx, req)
	require.NoError(t, err, "cache invalidation failure must not undo durable commit")
	require.Greater(t, second.Version, first.Version)
	_, hit, err = cache.LoadSnapshot(ctx, req.UserId, req.ConversationId, first.Version, 1)
	require.NoError(t, err)
	require.True(t, hit, "fault did not preserve the stale cache")
	history, err := s.store.History(ctx, req.UserId, req.ConversationId, 0)
	require.NoError(t, err)
	require.Len(t, history, 4)
	require.Equal(t, "second", history[3].Content)
	_, hit, err = cache.LoadSnapshot(ctx, req.UserId, req.ConversationId, first.Version, 1)
	require.NoError(t, err)
	require.False(t, hit, "reader reused the stale version")
	// An unavailable version source must not turn a populated Redis cache into
	// an authoritative database. Again, close only this test's extra client pool.
	db, pool, err := cfg.openPostgres(ctx)
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	unavailable := memory.NewTiered(memory.NewPostgres(db, memory.Conf{}), cache, memory.Conf{})
	_, err = unavailable.History(ctx, req.UserId, req.ConversationId, 0)
	require.Error(t, err)
}

func testVectorRecovery(t *testing.T, cfg targetConfig, s *openedStore) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	profile := product_vectors.Profile{Fingerprint: cfg.RunID + cfg.RunID, Dimensions: 3}
	model := product_vectors.NewProductVectorsModel(s.pg, profile)
	require.NoError(t, model.Initialize(ctx))
	row := func(id, hash string) product_vectors.ProductVectors {
		return product_vectors.ProductVectors{SkuId: id, ProductId: "synthetic", Content: "键盘",
			Metadata: `{"stock":3}`, Embedding: pgvector.NewVector([]float32{1, 0, 0}), ContentHash: hash}
	}
	require.NoError(t, model.WithSync(ctx, profile.Fingerprint, func(sync product_vectors.SyncStore) error {
		_, err := sync.PublishSync(ctx, product_vectors.SyncBatch{Complete: true,
			Upserts: []product_vectors.ProductVectors{row("keep", "old"), row("stale", "old")}, KeepIDs: []string{"keep", "stale"}})
		return err
	}))
	require.NoError(t, model.WithSync(ctx, profile.Fingerprint, func(sync product_vectors.SyncStore) error {
		before, err := sync.ListHashes(ctx)
		require.NoError(t, err)
		pruned, err := sync.PublishSync(ctx, product_vectors.SyncBatch{Complete: true,
			Upserts: []product_vectors.ProductVectors{row("keep", "new")}, KeepIDs: []string{"keep", "missing"},
			MetadataUpdates: []product_vectors.MetadataUpdate{{SkuId: "missing", Metadata: `{}`}}})
		require.Error(t, err)
		require.Zero(t, pruned)
		after, err := sync.ListHashes(ctx)
		require.NoError(t, err)
		require.Equal(t, before, after, "partial publication overwrote or pruned old index")
		return nil
	}))
	// Reopen an independent pool after failure and publish a complete recovery.
	db, pool, err := cfg.openPostgres(ctx)
	require.NoError(t, err)
	defer pool.Close()
	recovered := product_vectors.NewProductVectorsModel(db, profile)
	require.NoError(t, recovered.Initialize(ctx))
	require.NoError(t, recovered.WithSync(ctx, profile.Fingerprint, func(sync product_vectors.SyncStore) error {
		pruned, err := sync.PublishSync(ctx, product_vectors.SyncBatch{Complete: true,
			Upserts: []product_vectors.ProductVectors{row("keep", "new")}, KeepIDs: []string{"keep"}})
		require.NoError(t, err)
		require.EqualValues(t, 1, pruned)
		hashes, err := sync.ListHashes(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"keep": "new"}, hashes)
		return nil
	}))
	results, err := recovered.SearchByVector(ctx, []float32{1, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "keep", results[0].SkuId)
	require.Greater(t, results[0].Score, 0.99)
}
