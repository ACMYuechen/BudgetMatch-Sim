package product_index

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/mall/indexcontract"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A real database/sql + GORM path over an offline driver double. This proves
// transaction options/pinning, not PostgreSQL MVCC; the gated test covers MVCC.
type snapshotDB struct {
	mu         sync.Mutex
	options    []driver.TxOptions
	events     []string
	args       [][]driver.NamedValue
	pages      [][]Entry
	queryCount int
	fail       string
}
type snapshotConnector struct{ db *snapshotDB }
type snapshotDriver struct{ db *snapshotDB }
type snapshotConn struct {
	db   *snapshotDB
	inTx bool
}
type snapshotTx struct{ c *snapshotConn }
type snapshotRows struct{ rows []Entry }
type snapshotStatement struct {
	conn  *snapshotConn
	query string
}

var errSnapshotTest = errors.New("injected database error")

func (d *snapshotDriver) Open(string) (driver.Conn, error) {
	return (&snapshotConnector{d.db}).Connect(context.Background())
}
func (c *snapshotConnector) Driver() driver.Driver { return &snapshotDriver{c.db} }
func (c *snapshotConnector) Connect(context.Context) (driver.Conn, error) {
	return &snapshotConn{db: c.db}, nil
}
func (c *snapshotConn) Prepare(query string) (driver.Stmt, error) {
	return &snapshotStatement{conn: c, query: query}, nil
}
func (s *snapshotStatement) Close() error  { return nil }
func (s *snapshotStatement) NumInput() int { return -1 }
func (s *snapshotStatement) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("unexpected write")
}
func (s *snapshotStatement) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.QueryContext(context.Background(), s.query, named)
}
func (s *snapshotStatement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.QueryContext(ctx, s.query, args)
}
func (c *snapshotConn) Close() error { return nil }
func (c *snapshotConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *snapshotConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	c.db.events = append(c.db.events, "begin")
	c.db.options = append(c.db.options, opts)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.db.fail == "begin" {
		return nil, errSnapshotTest
	}
	if c.inTx {
		return nil, errors.New("nested transaction")
	}
	c.inTx = true
	return &snapshotTx{c}, nil
}
func (c *snapshotConn) QueryContext(ctx context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	if !c.inTx {
		return nil, errors.New("query escaped snapshot transaction")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.db.events = append(c.db.events, "query")
	c.db.args = append(c.db.args, append([]driver.NamedValue(nil), args...))
	c.db.queryCount++
	if c.db.fail == fmt.Sprintf("query-%d", c.db.queryCount) {
		return nil, errSnapshotTest
	}
	if len(c.db.pages) == 0 {
		return &snapshotRows{}, nil
	}
	rows := c.db.pages[0]
	c.db.pages = c.db.pages[1:]
	return &snapshotRows{rows}, nil
}
func (tx *snapshotTx) Commit() error {
	tx.c.db.mu.Lock()
	defer tx.c.db.mu.Unlock()
	tx.c.db.events = append(tx.c.db.events, "commit")
	tx.c.inTx = false
	if tx.c.db.fail == "commit" {
		return errSnapshotTest
	}
	return nil
}
func (tx *snapshotTx) Rollback() error {
	tx.c.db.mu.Lock()
	defer tx.c.db.mu.Unlock()
	tx.c.db.events = append(tx.c.db.events, "rollback")
	tx.c.inTx = false
	return nil
}
func (r *snapshotRows) Columns() []string {
	return []string{"sku_id", "product_id", "product_name", "product_content", "provider", "sku_name", "specs", "price", "stock", "sold"}
}
func (r *snapshotRows) Close() error { return nil }
func (r *snapshotRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	row := r.rows[0]
	r.rows = r.rows[1:]
	copy(dest, []driver.Value{row.SkuId, row.ProductId, row.ProductName, row.ProductContent, row.Provider, row.SkuName, row.Specs, row.Price, row.Stock, row.Sold})
	return nil
}
func snapshotModel(t *testing.T, state *snapshotDB) *Model {
	return snapshotModelWithPrepare(t, state, false)
}
func snapshotModelWithPrepare(t *testing.T, state *snapshotDB, prepare bool) *Model {
	t.Helper()
	pool := sql.OpenDB(&snapshotConnector{state})
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard, PrepareStmt: prepare})
	require.NoError(t, err)
	return NewModel(db)
}
func snapshotTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestSnapshotUsesOneReadOnlyRepeatableReadTransaction(t *testing.T) {
	for _, prepare := range []bool{false, true} {
		t.Run(fmt.Sprintf("prepare=%v", prepare), func(t *testing.T) {
			state := &snapshotDB{pages: [][]Entry{{{SkuId: "a", ProductId: "p1"}, {SkuId: "b", ProductId: "p1"}}, {{SkuId: "c", ProductId: "p2"}}}}
			m := snapshotModelWithPrepare(t, state, prepare)
			var ids []string
			err := m.ScanSnapshot(snapshotTestContext(t), 2, func(rows []Entry) error {
				state.mu.Lock()
				defer state.mu.Unlock()
				require.NotContains(t, state.events, "commit")
				for _, row := range rows {
					ids = append(ids, row.SkuId)
				}
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, []string{"a", "b", "c"}, ids)
			require.Equal(t, []driver.TxOptions{{Isolation: driver.IsolationLevel(sql.LevelRepeatableRead), ReadOnly: true}}, state.options)
			require.Equal(t, []string{"begin", "query", "query", "commit"}, state.events)
			require.Equal(t, []driver.NamedValue{{Ordinal: 1, Value: int64(1)}, {Ordinal: 2, Value: int64(1)}, {Ordinal: 3, Value: "b"}, {Ordinal: 4, Value: int64(2)}}, state.args[1])
		})
	}
}

func TestSnapshotEmptyAndExactMultiple(t *testing.T) {
	for _, count := range []int{0, 2, 4} {
		state := &snapshotDB{}
		for i := 0; i < count; i += 2 {
			state.pages = append(state.pages, []Entry{{SkuId: fmt.Sprintf("s%d", i)}, {SkuId: fmt.Sprintf("s%d", i+1)}})
		}
		m := snapshotModel(t, state)
		seen := 0
		require.NoError(t, m.ScanSnapshot(snapshotTestContext(t), 2, func(rows []Entry) error { seen += len(rows); return nil }))
		require.Equal(t, count, seen)
		require.Equal(t, count/2+1, state.queryCount)
	}
}

func TestSnapshotRejectsInheritedTransaction(t *testing.T) {
	for _, prepare := range []bool{false, true} {
		state := &snapshotDB{}
		m := snapshotModelWithPrepare(t, state, prepare)
		tx := m.conn.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		require.NoError(t, tx.Error)
		err := NewModel(tx).ScanSnapshot(snapshotTestContext(t), 1, func([]Entry) error { t.Fatal("nested scan read data"); return nil })
		require.ErrorContains(t, err, "requires its own transaction")
		require.Equal(t, []string{"begin"}, state.events, "must not use a savepoint and silently inherit weaker isolation")
		require.NoError(t, tx.Rollback().Error)
	}
}

func TestSnapshotFailureRollsBackAndReleasesAdmission(t *testing.T) {
	for _, failure := range []string{"begin", "query-1", "query-2", "commit", "callback", "panic"} {
		t.Run(failure, func(t *testing.T) {
			state := &snapshotDB{fail: failure, pages: [][]Entry{{{SkuId: "a"}}, {{SkuId: "b"}}}}
			m := snapshotModel(t, state)
			run := func() error {
				return m.ScanSnapshot(snapshotTestContext(t), 1, func([]Entry) error {
					if failure == "panic" {
						panic("test panic")
					}
					if failure == "callback" {
						return errSnapshotTest
					}
					return nil
				})
			}
			if failure == "panic" {
				require.Panics(t, func() { _ = run() })
			} else {
				require.ErrorIs(t, run(), errSnapshotTest)
			}
			if failure != "begin" && failure != "commit" {
				require.Equal(t, "rollback", state.events[len(state.events)-1])
				require.NotContains(t, state.events, "commit")
			}
			state.fail = ""
			state.pages = nil
			require.NoError(t, m.ScanSnapshot(snapshotTestContext(t), 1, func([]Entry) error { return nil }))
		})
	}
}

func TestSnapshotAdmissionCancellationAndDeadline(t *testing.T) {
	state := &snapshotDB{pages: [][]Entry{{{SkuId: "a"}}}}
	m := snapshotModel(t, state)
	ctx, cancel := context.WithCancel(snapshotTestContext(t))
	err := m.ScanSnapshot(ctx, 1, func([]Entry) error {
		require.ErrorIs(t, m.ScanSnapshot(ctx, 1, func([]Entry) error { t.Fatal("busy scan visited"); return nil }), ErrSnapshotBusy)
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	// database/sql may finish rollback on its cancellation goroutine.
	require.Eventually(t, func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		for _, e := range state.events {
			if e == "rollback" {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	state.mu.Lock()
	require.Equal(t, 1, state.queryCount)
	state.mu.Unlock()
	require.NoError(t, m.ScanSnapshot(snapshotTestContext(t), 1, func([]Entry) error { return nil }))
	for _, ctx := range []context.Context{context.Background(), ctx} {
		require.Error(t, NewModel(nil).ScanSnapshot(ctx, 1, func([]Entry) error { return nil }))
	}
	longCtx, longCancel := context.WithTimeout(context.Background(), time.Minute)
	defer longCancel()
	require.Error(t, NewModel(nil).ScanSnapshot(longCtx, 1, func([]Entry) error { return nil }))
	for _, size := range []int{-1, 0, 201} {
		require.Error(t, NewModel(nil).ScanSnapshot(snapshotTestContext(t), size, func([]Entry) error { return nil }))
	}
	require.Error(t, NewModel(nil).ScanSnapshot(snapshotTestContext(t), 1, nil))
}

func TestSnapshotRejectsInvalidProgressAndItemOverflow(t *testing.T) {
	for _, rows := range [][]Entry{{{SkuId: ""}}, {{SkuId: " a"}}, {{SkuId: "a"}, {SkuId: "a"}}, {{SkuId: "b"}, {SkuId: "a"}}} {
		state := &snapshotDB{pages: [][]Entry{rows}}
		m := snapshotModel(t, state)
		err := m.ScanSnapshot(snapshotTestContext(t), 2, func([]Entry) error { t.Fatal("invalid page delivered"); return nil })
		require.Error(t, err)
		require.NotContains(t, state.events, "commit")
	}
	state := &snapshotDB{}
	for i := 0; i <= indexcontract.MaxItems; i += 200 {
		page := make([]Entry, 200)
		for j := range page {
			page[j] = Entry{SkuId: fmt.Sprintf("s%08d", i+j)}
		}
		state.pages = append(state.pages, page)
	}
	seen := 0
	err := snapshotModel(t, state).ScanSnapshot(snapshotTestContext(t), 200, func(rows []Entry) error { seen += len(rows); return nil })
	require.ErrorIs(t, err, ErrSnapshotLimit)
	require.Equal(t, indexcontract.MaxItems, seen)
	require.NotContains(t, state.events, "commit")
}
