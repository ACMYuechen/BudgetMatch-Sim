package product_vectors

import (
	"context"
	"os"
	"testing"

	"github.com/pgvector/pgvector-go"
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
