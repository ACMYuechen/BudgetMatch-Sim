package product_vectors

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func preparedRow(id string) ProductVectors {
	return ProductVectors{SkuId: id, ProductId: "p1", Content: "商品", Metadata: `{"stock":3}`,
		Embedding: pgvector.NewVector([]float32{1, 0, 0}), ContentHash: "new-hash"}
}

func preparedBatch(count int) SyncBatch {
	batch := SyncBatch{Complete: true}
	for i := range count {
		row := preparedRow(fmt.Sprintf("sku-%03d", i))
		batch.Upserts = append(batch.Upserts, row)
		batch.KeepIDs = append(batch.KeepIDs, row.SkuId)
	}
	batch.MetadataUpdates = []MetadataUpdate{{SkuId: "same", Metadata: `{"stock":2}`}}
	batch.KeepIDs = append(batch.KeepIDs, "same")
	return batch
}

// An offline GORM connection records real dialect/callback SQL and transaction
// boundaries. It neither connects to Postgres nor proves server-side isolation;
// the separately gated integration test checks persisted rollback behavior.
type txTrace struct {
	events     []string
	queries    []string
	args       [][]any
	fail       string
	updateRows int64
	afterExec  func(int)
	committed  bool
}

type traceConn struct {
	gorm.ConnPool
	trace         *txTrace
	inTransaction bool
}
type tracePool struct{ *traceConn }
type traceTx struct{ *traceConn }

var errInjected = errors.New("injected SQL failure")

func (c *tracePool) BeginTx(ctx context.Context, _ *sql.TxOptions) (gorm.ConnPool, error) {
	c.trace.events = append(c.trace.events, "begin")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.trace.fail == "begin" {
		return nil, errInjected
	}
	return &traceTx{&traceConn{trace: c.trace, inTransaction: true}}, nil
}

func (c *traceConn) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if !c.inTransaction {
		return nil, errors.New("write escaped transaction")
	}
	verb := strings.ToLower(strings.Fields(query)[0])
	c.trace.events = append(c.trace.events, verb)
	c.trace.queries = append(c.trace.queries, query)
	c.trace.args = append(c.trace.args, args)
	if c.trace.afterExec != nil {
		c.trace.afterExec(len(c.trace.queries))
	}
	if c.trace.fail == verb {
		return nil, errInjected
	}
	if verb == "update" {
		return driver.RowsAffected(c.trace.updateRows), nil
	}
	return driver.RowsAffected(2), nil
}

func (c *traceConn) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected query in publication")
}

func (c *traceTx) Commit() error {
	c.trace.events = append(c.trace.events, "commit")
	if c.trace.fail == "commit" {
		return errInjected
	}
	c.trace.committed = true
	return nil
}

func (c *traceTx) Rollback() error {
	c.trace.events = append(c.trace.events, "rollback")
	return nil
}

func recordingModel(t *testing.T, trace *txTrace) *defaultProductVectorsModel {
	t.Helper()
	pool := &tracePool{&traceConn{trace: trace}}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Discard,
	})
	require.NoError(t, err)
	// These tests start at publication inside an already owned session. Session
	// admission/connection lifetime are exercised separately using database/sql.
	return &defaultProductVectorsModel{conn: db, ownedSession: true}
}

func TestPublishSyncSingleTransactionIncludesAllWrites(t *testing.T) {
	trace := &txTrace{updateRows: 1}
	m := recordingModel(t, trace)
	batch := preparedBatch(129)
	pruned, err := m.PublishSync(context.Background(), batch)
	require.NoError(t, err)
	require.EqualValues(t, 2, pruned)
	require.True(t, trace.committed)
	require.Equal(t, []string{"begin", "insert", "insert", "update", "delete", "commit"}, trace.events)
	require.Len(t, trace.args[0], 128*8, "each INSERT is bounded independently")
	require.Len(t, trace.args[1], 8)
	require.Contains(t, trace.queries[0], `ON CONFLICT ("sku_id") DO UPDATE`)
	require.Contains(t, trace.queries[2], `"metadata"=`)
	require.NotContains(t, trace.queries[2], `"embedding"=`)
	require.Contains(t, trace.queries[3], "sku_id NOT IN")
	for _, row := range batch.Upserts {
		require.True(t, row.CreatedAt.IsZero(), "GORM must not mutate the prepared batch")
		require.True(t, row.UpdatedAt.IsZero())
	}
}

func TestPublishSyncSQLFailuresRollbackAndStopFollowingWrites(t *testing.T) {
	for _, tc := range []struct {
		fail   string
		events []string
	}{
		{"begin", []string{"begin"}},
		{"insert", []string{"begin", "insert", "rollback"}},
		{"update", []string{"begin", "insert", "update", "rollback"}},
		{"delete", []string{"begin", "insert", "update", "delete", "rollback"}},
		{"commit", []string{"begin", "insert", "update", "delete", "commit", "rollback"}},
	} {
		t.Run(tc.fail, func(t *testing.T) {
			trace := &txTrace{updateRows: 1, fail: tc.fail}
			pruned, err := recordingModel(t, trace).PublishSync(context.Background(), preparedBatch(1))
			require.ErrorIs(t, err, errInjected)
			require.Zero(t, pruned)
			require.False(t, trace.committed)
			require.Equal(t, tc.events, trace.events)
		})
	}
}

func TestPublishSyncMissingRefreshTargetAbortsPrune(t *testing.T) {
	trace := &txTrace{updateRows: 0}
	pruned, err := recordingModel(t, trace).PublishSync(context.Background(), preparedBatch(1))
	require.ErrorContains(t, err, "refresh target changed")
	require.Zero(t, pruned)
	require.Equal(t, []string{"begin", "insert", "update", "rollback"}, trace.events)
}

func TestPublishSyncCancellationRollsBackBeforeCommit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cancelAfter int
		events      []string
	}{
		{"before begin", 0, nil},
		{"between chunks", 1, []string{"begin", "insert", "rollback"}},
		{"before refresh", 2, []string{"begin", "insert", "insert", "rollback"}},
		{"before prune", 3, []string{"begin", "insert", "insert", "update", "rollback"}},
		{"after prune", 4, []string{"begin", "insert", "insert", "update", "delete", "rollback"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			trace := &txTrace{updateRows: 1, afterExec: func(n int) {
				if n == tc.cancelAfter {
					cancel()
				}
			}}
			if tc.cancelAfter == 0 {
				cancel()
			}
			pruned, err := recordingModel(t, trace).PublishSync(ctx, preparedBatch(129))
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, pruned)
			require.False(t, trace.committed)
			require.Equal(t, tc.events, trace.events)
		})
	}
}

func TestPublishSyncRejectsInvalidBatchBeforeBegin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*SyncBatch)
	}{
		{"incomplete", func(b *SyncBatch) { b.Complete = false }},
		{"duplicate keep", func(b *SyncBatch) { b.KeepIDs = append(b.KeepIDs, b.KeepIDs[0]) }},
		{"invalid keep", func(b *SyncBatch) { b.KeepIDs[0] = " " }},
		{"missing keep", func(b *SyncBatch) { b.KeepIDs = b.KeepIDs[:1] }},
		{"uncovered keep", func(b *SyncBatch) { b.KeepIDs = append(b.KeepIDs, "unknown") }},
		{"duplicate row", func(b *SyncBatch) { b.Upserts = append(b.Upserts, b.Upserts[0]) }},
		{"overlap refresh", func(b *SyncBatch) { b.MetadataUpdates[0].SkuId = b.Upserts[0].SkuId }},
		{"invalid product", func(b *SyncBatch) { b.Upserts[0].ProductId = "" }},
		{"empty content", func(b *SyncBatch) { b.Upserts[0].Content = "\t" }},
		{"missing hash", func(b *SyncBatch) { b.Upserts[0].ContentHash = "" }},
		{"overlong hash", func(b *SyncBatch) { b.Upserts[0].ContentHash = strings.Repeat("a", 65) }},
		{"invalid JSON", func(b *SyncBatch) { b.Upserts[0].Metadata = "invalid" }},
		{"null metadata", func(b *SyncBatch) { b.MetadataUpdates[0].Metadata = "null" }},
		{"array metadata", func(b *SyncBatch) { b.MetadataUpdates[0].Metadata = "[]" }},
		{"missing vector", func(b *SyncBatch) { b.Upserts[0].Embedding = pgvector.NewVector(nil) }},
		{"zero vector", func(b *SyncBatch) { b.Upserts[0].Embedding = pgvector.NewVector([]float32{0, 0, 0}) }},
		{"NaN vector", func(b *SyncBatch) { b.Upserts[0].Embedding = pgvector.NewVector([]float32{float32(math.NaN()), 0, 0}) }},
		{"infinite vector", func(b *SyncBatch) { b.Upserts[0].Embedding = pgvector.NewVector([]float32{float32(math.Inf(1)), 0, 0}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := preparedBatch(1)
			tc.change(&batch)
			trace := &txTrace{updateRows: 1}
			pruned, err := recordingModel(t, trace).PublishSync(context.Background(), batch)
			require.Error(t, err)
			require.Zero(t, pruned)
			require.Empty(t, trace.events)
		})
	}
}

func TestPublishSyncEmptyCatalogRequiresExplicitCompletion(t *testing.T) {
	trace := &txTrace{}
	m := recordingModel(t, trace)
	_, err := m.PublishSync(context.Background(), SyncBatch{})
	require.Error(t, err)
	require.Empty(t, trace.events)
	_, err = m.PublishSync(context.Background(), SyncBatch{Complete: true})
	require.NoError(t, err)
	require.Equal(t, []string{"begin", "delete", "commit"}, trace.events)
	require.Contains(t, trace.queries[0], "WHERE 1 = 1")
}
