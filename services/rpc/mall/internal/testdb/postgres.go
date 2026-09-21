// Package testdb provides isolated schemas for opt-in Mall integration tests.
// It never loads .env or falls back to the application's database connection.
package testdb

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

// Open pins a connection to a fresh schema in an explicitly supplied disposable
// database. Each test cleans up only its own schema, including after failures.
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("RAG_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set RAG_TEST_PG_DSN to a disposable database for PostgreSQL integration tests")
	}
	config := &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard}
	root, err := gorm.Open(postgres.Open(dsn), config)
	if err != nil {
		t.Fatal("cannot open integration database; connection details suppressed")
	}
	pool, err := root.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		t.Fatal("cannot connect to integration database; connection details suppressed")
	}
	schema := "test_mall_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = pool.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// The identifier is generated above, never taken from configuration.
		_, err := pool.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, err)
	})
	conn, err := pool.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	_, err = conn.ExecContext(ctx, "SET search_path TO "+schema)
	require.NoError(t, err)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), config)
	require.NoError(t, err)
	return db
}
