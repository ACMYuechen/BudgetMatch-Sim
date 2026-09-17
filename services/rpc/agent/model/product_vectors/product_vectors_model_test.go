package product_vectors

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func testProfile() Profile { return Profile{Fingerprint: strings.Repeat("a", 64), Dimensions: 3} }

// Requires an explicitly configured DISPOSABLE database. Integration tests create
// and remove vector/profile tables; no .env is loaded.
func newTestModel(t *testing.T) *defaultProductVectorsModel {
	t.Helper()
	dsn := os.Getenv("RAG_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("RAG_TEST_PG_DSN not set, skipping pgvector integration test")
	}
	conn, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Migrator().DropTable(&ProductVectors{})
		_ = conn.Exec("DROP TABLE IF EXISTS product_vector_profile").Error
		pool, _ := conn.DB()
		_ = pool.Close()
	})
	return &defaultProductVectorsModel{conn: conn, profile: testProfile()}
}

func TestPgVectorSyncPublicationRollback(t *testing.T) {
	m := newTestModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, m.Initialize(ctx))
	require.NoError(t, m.WithSync(ctx, m.profile.Fingerprint, func(s SyncStore) error {
		rows := []ProductVectors{preparedRow("same"), preparedRow("stale")}
		for i := range rows {
			rows[i].ContentHash = "old-hash"
		}
		require.NoError(t, s.Upsert(ctx, rows))
		before, err := s.ListHashes(ctx)
		require.NoError(t, err)
		batch := preparedBatch(1)
		batch.MetadataUpdates = append(batch.MetadataUpdates, MetadataUpdate{SkuId: "missing", Metadata: `{}`})
		batch.KeepIDs = append(batch.KeepIDs, "missing")
		pruned, err := s.PublishSync(ctx, batch)
		require.ErrorContains(t, err, "refresh target changed")
		require.Zero(t, pruned)
		after, err := s.ListHashes(ctx)
		require.NoError(t, err)
		require.Equal(t, before, after)
		actual, err := m.SearchByVector(ctx, []float32{1, 0, 0}, 10)
		require.NoError(t, err)
		for _, row := range actual {
			require.JSONEq(t, `{"stock":3}`, row.Metadata)
		}
		pruned, err = s.PublishSync(ctx, preparedBatch(1))
		require.NoError(t, err)
		require.EqualValues(t, 1, pruned)
		after, err = s.ListHashes(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"same": "old-hash", "sku-000": "new-hash"}, after)
		actual, err = m.SearchByVector(ctx, []float32{1, 0, 0}, 10)
		require.NoError(t, err)
		for _, row := range actual {
			if row.SkuId == "same" {
				require.JSONEq(t, `{"stock":2}`, row.Metadata)
			}
		}
		pruned, err = s.PublishSync(ctx, SyncBatch{Complete: true})
		require.NoError(t, err)
		require.EqualValues(t, 2, pruned)
		after, err = s.ListHashes(ctx)
		require.NoError(t, err)
		require.Empty(t, after)
		return nil
	}))
}

func TestPgVectorRoundTrip(t *testing.T) {
	m := newTestModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, m.Initialize(ctx))
	require.NoError(t, m.Initialize(ctx))
	rows := []ProductVectors{
		{SkuId: "s1", ProductId: "p1", Content: "键盘", Metadata: `{"name":"键盘"}`, Embedding: pgvector.NewVector([]float32{1, 0, 0}), ContentHash: "h1"},
		{SkuId: "s2", ProductId: "p1", Content: "台灯", Metadata: `{"name":"台灯"}`, Embedding: pgvector.NewVector([]float32{0, 1, 0}), ContentHash: "h2"},
	}
	require.NoError(t, m.WithSync(ctx, m.profile.Fingerprint, func(s SyncStore) error {
		require.NoError(t, s.Upsert(ctx, rows))
		got, err := m.SearchByVector(ctx, []float32{1, 0, 0}, 2)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, "s1", got[0].SkuId)
		require.Greater(t, got[0].Score, 0.99)
		hashes, err := s.ListHashes(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"s1": "h1", "s2": "h2"}, hashes)
		pruned, err := s.DeleteNotIn(ctx, []string{"s1"})
		require.NoError(t, err)
		require.EqualValues(t, 1, pruned)
		return nil
	}))
	// Reject dimension changes and same-dimensional model changes, preserving data.
	for _, profile := range []Profile{
		{Fingerprint: strings.Repeat("b", 64), Dimensions: 4},
		{Fingerprint: strings.Repeat("b", 64), Dimensions: 3},
	} {
		require.ErrorIs(t, NewProductVectorsModel(m.conn, profile).Initialize(ctx), ErrProfileMismatch)
	}
	require.NoError(t, m.WithSync(ctx, m.profile.Fingerprint, func(s SyncStore) error {
		hashes, err := s.ListHashes(ctx)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"s1": "h1"}, hashes)
		return nil
	}))
}

func TestPgVectorSessionExclusionAndLostOwner(t *testing.T) {
	m := newTestModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, m.Initialize(ctx))
	other := NewProductVectorsModel(m.conn, m.profile)
	ownerCtx, cancelOwner := context.WithCancel(ctx)
	defer cancelOwner()
	var escaped SyncStore
	require.NoError(t, m.WithSync(ownerCtx, m.profile.Fingerprint, func(s SyncStore) error {
		escaped = s
		require.ErrorIs(t, other.WithSync(ctx, m.profile.Fingerprint, func(SyncStore) error {
			t.Error("contender entered protected scan")
			return nil
		}), ErrSyncBusy)
		cancelOwner()
		// Allow PostgreSQL to observe session end before the contender retries.
		require.Eventually(t, func() bool {
			return other.WithSync(ctx, m.profile.Fingerprint, func(SyncStore) error { return nil }) == nil
		}, time.Second, 10*time.Millisecond)
		// Even an erroneous uncanceled context cannot revive the old connection.
		_, err := s.PublishSync(ctx, SyncBatch{Complete: true})
		require.Error(t, err)
		return nil
	}))
	_, err := escaped.ListHashes(ctx)
	require.Error(t, err)
}
