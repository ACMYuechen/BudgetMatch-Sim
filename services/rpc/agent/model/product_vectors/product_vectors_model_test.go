package product_vectors

import (
	"context"
	"os"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newTestModel 连接真实 pgvector 库；未设置 RAG_TEST_PG_DSN 时跳过。
// 本地运行示例：
//
//	RAG_TEST_PG_DSN="host=127.0.0.1 user=root password=123456 dbname=budgetmatch-sim port=5432 sslmode=disable" \
//	  go test ./services/rpc/agent/model/product_vectors/
func newTestModel(t *testing.T) ProductVectorsModel {
	t.Helper()
	dsn := os.Getenv("RAG_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("RAG_TEST_PG_DSN not set, skipping pgvector integration test")
	}
	conn, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Migrator().DropTable(&ProductVectors{})
	})
	return NewProductVectorsModel(conn)
}

// Requires the same disposable Postgres/pgvector database as newTestModel.
// Without explicit RAG_TEST_PG_DSN this test skips; offline SQL traces are not
// substituted for a claim about persisted database state.
func TestPgVectorSyncPublicationRollback(t *testing.T) {
	m := newTestModel(t)
	ctx := context.Background()
	require.NoError(t, m.CreateTable(3))
	rows := []ProductVectors{preparedRow("same"), preparedRow("stale")}
	for i := range rows {
		rows[i].ContentHash = "old-hash"
	}
	require.NoError(t, m.Upsert(ctx, rows))
	before, err := m.ListHashes(ctx)
	require.NoError(t, err)
	// The upsert succeeds inside the transaction; a missing metadata refresh
	// target must roll it back and preserve the stale row instead of pruning it.
	batch := preparedBatch(1)
	batch.MetadataUpdates = append(batch.MetadataUpdates, MetadataUpdate{SkuId: "missing", Metadata: `{}`})
	batch.KeepIDs = append(batch.KeepIDs, "missing")
	pruned, err := m.PublishSync(ctx, batch)
	require.ErrorContains(t, err, "refresh target changed")
	require.Zero(t, pruned)
	after, err := m.ListHashes(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	actual, err := m.SearchByVector(ctx, []float32{1, 0, 0}, 10)
	require.NoError(t, err)
	for _, row := range actual {
		require.JSONEq(t, `{"stock":3}`, row.Metadata)
	}
	// The same scan succeeds when every refresh target exists.
	pruned, err = m.PublishSync(ctx, preparedBatch(1))
	require.NoError(t, err)
	require.EqualValues(t, 1, pruned)
	after, err = m.ListHashes(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"same": "old-hash", "sku-000": "new-hash"}, after)
	actual, err = m.SearchByVector(ctx, []float32{1, 0, 0}, 10)
	require.NoError(t, err)
	for _, row := range actual {
		if row.SkuId == "same" {
			require.JSONEq(t, `{"stock":2}`, row.Metadata)
		}
	}
	pruned, err = m.PublishSync(ctx, SyncBatch{Complete: true})
	require.NoError(t, err)
	require.EqualValues(t, 2, pruned)
	after, err = m.ListHashes(ctx)
	require.NoError(t, err)
	require.Empty(t, after)
}

// TestPgVectorRoundTrip 验证建表幂等、维度变更重建、upsert 与余弦排序。
func TestPgVectorRoundTrip(t *testing.T) {
	ctx := context.Background()
	m := newTestModel(t)

	// 建表幂等
	if err := m.CreateTable(3); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	if err := m.CreateTable(3); err != nil {
		t.Fatalf("CreateTable() second call error = %v", err)
	}

	rows := []ProductVectors{
		{SkuId: "s1", ProductId: "p1", Content: "键盘", Metadata: `{"name":"键盘"}`, Embedding: pgvector.NewVector([]float32{1, 0, 0}), ContentHash: "h1"},
		{SkuId: "s2", ProductId: "p1", Content: "台灯", Metadata: `{"name":"台灯"}`, Embedding: pgvector.NewVector([]float32{0, 1, 0}), ContentHash: "h2"},
	}
	if err := m.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	// 与 [1,0,0] 最相似的应是 s1，且分数接近 1
	got, err := m.SearchByVector(ctx, []float32{1, 0, 0}, 2)
	if err != nil {
		t.Fatalf("SearchByVector() error = %v", err)
	}
	if len(got) != 2 || got[0].SkuId != "s1" || got[0].Score < 0.99 {
		t.Fatalf("unexpected search result: %+v", got)
	}

	hashes, err := m.ListHashes(ctx)
	if err != nil || len(hashes) != 2 || hashes["s1"] != "h1" {
		t.Fatalf("ListHashes() = %v, %v", hashes, err)
	}

	pruned, err := m.DeleteNotIn(ctx, []string{"s1"})
	if err != nil || pruned != 1 {
		t.Fatalf("DeleteNotIn() = %d, %v", pruned, err)
	}

	// 维度变更 → 重建为空表
	if err := m.CreateTable(4); err != nil {
		t.Fatalf("CreateTable(dim change) error = %v", err)
	}
	hashes, err = m.ListHashes(ctx)
	if err != nil || len(hashes) != 0 {
		t.Fatalf("expected empty table after dimension rebuild, got %v, %v", hashes, err)
	}
}
