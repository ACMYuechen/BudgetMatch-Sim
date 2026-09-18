package product_index

import (
	"context"
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

// Opt-in only. Creates/removes just its random schema in a disposable DB.
func TestPostgresCandidateChecksObserveCommittedChanges(t *testing.T) {
	dsn := os.Getenv("BUDGETMATCH_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set BUDGETMATCH_TEST_POSTGRES_DSN to a disposable database for candidate freshness tests")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	require.NoError(t, err)
	pool, err := root.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	schemaName := "test_candidate_check_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = conn.ExecContext(ctx, "CREATE SCHEMA "+schemaName)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		// Exact test-created identifier, never a user-provided schema.
		_, err := pool.ExecContext(cleanupCtx, "DROP SCHEMA "+schemaName+" CASCADE")
		require.NoError(t, err)
	})
	for _, query := range []string{
		"SET search_path TO " + schemaName,
		"CREATE TABLE products (id text PRIMARY KEY,name text,status smallint,deleted_at timestamptz)",
		"CREATE TABLE product_skus (id text PRIMARY KEY,product_id text,name text,price bigint,stock integer,sold integer,status smallint,deleted_at timestamptz)",
		"INSERT INTO products VALUES ('p','keyboard',1,NULL)",
		"INSERT INTO product_skus VALUES ('a','p','red',100,3,0,1,NULL)",
	} {
		_, err = conn.ExecContext(ctx, query)
		require.NoError(t, err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	require.NoError(t, err)
	model := NewModel(db)
	rows, err := model.FindActiveCandidates(ctx, []string{"a", "missing"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 100, rows[0].Price)
	_, err = conn.ExecContext(ctx, "UPDATE product_skus SET price=300,stock=1 WHERE id='a'")
	require.NoError(t, err)
	rows, err = model.FindActiveCandidates(ctx, []string{"a"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 300, rows[0].Price)
	require.EqualValues(t, 1, rows[0].Stock)
	for _, tc := range []struct{ name, update, reset string }{
		{"parent off-shelf", "UPDATE products SET status=0", "UPDATE products SET status=1"},
		{"sku off-shelf", "UPDATE product_skus SET status=0", "UPDATE product_skus SET status=1"},
		{"parent deleted", "UPDATE products SET deleted_at=now()", "UPDATE products SET deleted_at=NULL"},
		{"sku deleted", "UPDATE product_skus SET deleted_at=now()", "UPDATE product_skus SET deleted_at=NULL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := conn.ExecContext(ctx, tc.update)
			require.NoError(t, err)
			rows, err := model.FindActiveCandidates(ctx, []string{"a"})
			require.NoError(t, err)
			require.Empty(t, rows)
			_, err = conn.ExecContext(ctx, tc.reset)
			require.NoError(t, err)
		})
	}
	_, err = conn.ExecContext(ctx, "DELETE FROM product_skus WHERE id='a'")
	require.NoError(t, err)
	rows, err = model.FindActiveCandidates(ctx, []string{"a"})
	require.NoError(t, err)
	require.Empty(t, rows)
}
