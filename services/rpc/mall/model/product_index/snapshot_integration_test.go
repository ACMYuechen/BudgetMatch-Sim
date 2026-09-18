package product_index

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Only run with an explicitly supplied disposable PostgreSQL test database.
// The test creates and drops its own randomly named schema, never public tables.
func TestPostgresCatalogSnapshotConsistency(t *testing.T) {
	dsn := os.Getenv("BUDGETMATCH_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set BUDGETMATCH_TEST_POSTGRES_DSN to a disposable database for snapshot MVCC tests")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	require.NoError(t, err)
	admin, err := adminDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, admin.PingContext(ctx))
	schemaName := "test_product_snapshot_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schemaName)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		// Exact schema created by this test, not a user-provided identifier.
		_, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA "+schemaName+" CASCADE")
		require.NoError(t, err)
	})
	for _, ddl := range []string{
		"CREATE TABLE " + schemaName + ".products (id text PRIMARY KEY,name text,content jsonb,providor text,status smallint,deleted_at timestamptz)",
		"CREATE TABLE " + schemaName + ".product_skus (id text PRIMARY KEY,product_id text,name text,specs jsonb,price bigint,stock integer,sold integer,status smallint,deleted_at timestamptz)",
		"INSERT INTO " + schemaName + ".products VALUES ('p1','old','{}','brand',1,NULL),('p2','second','{}','brand',1,NULL)",
		"INSERT INTO " + schemaName + ".product_skus VALUES ('a','p1','a','{}',100,3,0,1,NULL),('b','p1','b','{}',200,4,0,1,NULL),('c','p2','c','{}',300,5,0,1,NULL)",
	} {
		_, err = admin.ExecContext(ctx, ddl)
		require.NoError(t, err)
	}
	// Pin only this test's reader to its isolated schema. The concurrent writer
	// uses another connection and fully qualified table names. This entire pool
	// belongs to the test and is closed during cleanup, never a shared service pool.
	readConn, err := admin.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, readConn.Close()) })
	_, err = readConn.ExecContext(ctx, "SET search_path TO "+schemaName)
	require.NoError(t, err)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: readConn}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	require.NoError(t, err)
	m := NewModel(db)
	var first []Entry
	err = m.ScanSnapshot(ctx, 1, func(rows []Entry) error {
		first = append(first, rows...)
		if len(first) != 1 {
			return nil
		}
		// Commit changes from another physical connection between pages: edit a
		// shared parent, mutate future SKU facts, insert a phantom, delete a SKU.
		writer, err := admin.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			return err
		}
		defer writer.Rollback()
		for _, query := range []string{
			"UPDATE " + schemaName + ".products SET name='new' WHERE id='p1'",
			"UPDATE " + schemaName + ".product_skus SET price=999,stock=1 WHERE id='b'",
			"INSERT INTO " + schemaName + ".product_skus VALUES ('aa','p1','aa','{}',150,2,0,1,NULL)",
			"DELETE FROM " + schemaName + ".product_skus WHERE id='c'",
		} {
			if _, err = writer.ExecContext(ctx, query); err != nil {
				return err
			}
		}
		return writer.Commit()
	})
	require.NoError(t, err)
	require.Len(t, first, 3)
	require.Equal(t, []string{"a", "b", "c"}, []string{first[0].SkuId, first[1].SkuId, first[2].SkuId})
	require.Equal(t, "old", first[1].ProductName)
	require.EqualValues(t, 200, first[1].Price)
	require.EqualValues(t, 4, first[1].Stock)
	var second []Entry
	require.NoError(t, m.ScanSnapshot(ctx, 1, func(rows []Entry) error { second = append(second, rows...); return nil }))
	require.Len(t, second, 3)
	require.Equal(t, []string{"a", "aa", "b"}, []string{second[0].SkuId, second[1].SkuId, second[2].SkuId})
	require.Equal(t, "new", second[2].ProductName)
	require.EqualValues(t, 999, second[2].Price)
	require.EqualValues(t, 1, second[2].Stock)
}
