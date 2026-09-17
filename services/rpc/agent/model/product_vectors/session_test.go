package product_vectors

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A database/sql driver double verifies actual connection pinning/discard, GORM
// SQL and transaction calls without a Postgres server. It is NOT an emulator of
// PostgreSQL isolation, session pooling, failover or extension behavior.
type sessionSchema struct {
	dim       int
	profile   *Profile
	populated bool
}
type sessionEvent struct {
	id   int
	sql  string
	args []driver.NamedValue
	tx   bool
}
type sessionDatabase struct {
	mu               sync.Mutex
	schema           sessionSchema
	owner            *sessionConnection
	connections      []*sessionConnection
	events           []sessionEvent
	lockErr, execErr error
	failSQL          string
}
type sessionConnector struct{ db *sessionDatabase }
type sessionDriver struct{ db *sessionDatabase }
type sessionConnection struct {
	db      *sessionDatabase
	id      int
	closed  bool
	pending *sessionSchema
}
type sessionTx struct{ conn *sessionConnection }
type sessionRows struct {
	columns  []string
	values   [][]driver.Value
	position int
}
type sessionStatement struct {
	conn  *sessionConnection
	query string
}

func (d *sessionDriver) Open(string) (driver.Conn, error) {
	return (&sessionConnector{d.db}).Connect(context.Background())
}
func (c *sessionConnector) Driver() driver.Driver { return &sessionDriver{c.db} }
func (c *sessionConnector) Connect(context.Context) (driver.Conn, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	conn := &sessionConnection{db: c.db, id: len(c.db.connections) + 1}
	c.db.connections = append(c.db.connections, conn)
	return conn, nil
}
func (c *sessionConnection) Prepare(q string) (driver.Stmt, error) {
	return &sessionStatement{conn: c, query: q}, nil
}
func (s *sessionStatement) Close() error  { return nil }
func (s *sessionStatement) NumInput() int { return -1 }
func namedValues(args []driver.Value) []driver.NamedValue {
	result := make([]driver.NamedValue, len(args))
	for i, v := range args {
		result[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return result
}
func (s *sessionStatement) Exec(args []driver.Value) (driver.Result, error) {
	return s.conn.ExecContext(context.Background(), s.query, namedValues(args))
}
func (s *sessionStatement) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.QueryContext(context.Background(), s.query, namedValues(args))
}
func (s *sessionStatement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.ExecContext(ctx, s.query, args)
}
func (s *sessionStatement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.QueryContext(ctx, s.query, args)
}
func (c *sessionConnection) Close() error {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	if !c.closed {
		c.closed = true
		if c.db.owner == c {
			c.db.owner = nil
		}
		c.db.events = append(c.db.events, sessionEvent{id: c.id, sql: "close"})
	}
	return nil
}
func (c *sessionConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *sessionConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.closed {
		return nil, driver.ErrBadConn
	}
	if c.pending != nil {
		return nil, errors.New("nested transaction")
	}
	state := c.db.schema
	c.pending = &state
	c.db.events = append(c.db.events, sessionEvent{id: c.id, sql: "begin", tx: true})
	return &sessionTx{c}, nil
}
func (t *sessionTx) Commit() error {
	c := t.conn
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	if c.closed {
		return driver.ErrBadConn
	}
	c.db.schema = *c.pending
	c.pending = nil
	c.db.events = append(c.db.events, sessionEvent{id: c.id, sql: "commit"})
	return nil
}
func (t *sessionTx) Rollback() error {
	c := t.conn
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	c.pending = nil
	c.db.events = append(c.db.events, sessionEvent{id: c.id, sql: "rollback"})
	return nil
}
func (c *sessionConnection) state() *sessionSchema {
	if c.pending != nil {
		return c.pending
	}
	return &c.db.schema
}
func (c *sessionConnection) record(q string, args []driver.NamedValue) {
	c.db.events = append(c.db.events, sessionEvent{id: c.id, sql: q, args: args, tx: c.pending != nil})
}
func (c *sessionConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.closed {
		return nil, driver.ErrBadConn
	}
	c.record(query, args)
	switch {
	case strings.Contains(query, "pg_try_advisory_lock"):
		acquired := c.db.owner == nil || c.db.owner == c
		if acquired {
			c.db.owner = c
		}
		if c.db.lockErr != nil {
			return nil, c.db.lockErr
		} // Lock may be held even when its reply is lost.
		return &sessionRows{columns: []string{"pg_try_advisory_lock"}, values: [][]driver.Value{{acquired}}}, nil
	case strings.HasPrefix(query, "SELECT fingerprint"):
		r := &sessionRows{columns: []string{"fingerprint", "dimensions"}}
		if p := c.state().profile; p != nil {
			r.values = [][]driver.Value{{p.Fingerprint, int64(p.Dimensions)}}
		}
		return r, nil
	case strings.Contains(query, "SELECT atttypmod"):
		return &sessionRows{columns: []string{"atttypmod"}, values: [][]driver.Value{{int64(c.state().dim)}}}, nil
	case strings.Contains(query, "SELECT EXISTS (SELECT 1 FROM product_vectors)"):
		return &sessionRows{columns: []string{"populated"}, values: [][]driver.Value{{c.state().populated}}}, nil
	case strings.Contains(query, "embedding <=>"):
		return &sessionRows{columns: []string{"sku_id", "score"}}, nil
	case strings.Contains(query, "content_hash"):
		return &sessionRows{columns: []string{"sku_id", "content_hash"}}, nil
	default:
		return nil, errors.New("unexpected offline query")
	}
}
func (c *sessionConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.closed {
		return nil, driver.ErrBadConn
	}
	c.record(query, args)
	if c.db.failSQL != "" && strings.Contains(query, c.db.failSQL) {
		return nil, c.db.execErr
	}
	switch {
	case strings.HasPrefix(query, "CREATE TABLE IF NOT EXISTS product_vectors"):
		if c.state().dim == 0 {
			parts := regexp.MustCompile(`vector\((\d+)\)`).FindStringSubmatch(query)
			if len(parts) != 2 {
				return nil, errors.New("dimension missing")
			}
			c.state().dim, _ = strconv.Atoi(parts[1])
		}
	case strings.HasPrefix(query, "INSERT INTO product_vector_profile"):
		c.state().profile = &Profile{Fingerprint: args[0].Value.(string), Dimensions: int(args[1].Value.(int64))}
	case strings.HasPrefix(query, "CREATE "), strings.HasPrefix(query, "INSERT "), strings.HasPrefix(query, "UPDATE "), strings.HasPrefix(query, "DELETE "):
	default:
		return nil, errors.New("unexpected offline statement")
	}
	return driver.RowsAffected(1), nil
}
func (r *sessionRows) Columns() []string { return r.columns }
func (r *sessionRows) Close() error      { return nil }
func (r *sessionRows) Next(dest []driver.Value) error {
	if r.position == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.position])
	r.position++
	return nil
}

func sessionModel(t *testing.T, state *sessionDatabase, profile Profile, prepared bool) *defaultProductVectorsModel {
	t.Helper()
	pool := sql.OpenDB(&sessionConnector{state})
	pool.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = pool.Close() })
	conn, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
		DisableAutomaticPing: true, PrepareStmt: prepared, Logger: logger.Discard,
	})
	require.NoError(t, err)
	return &defaultProductVectorsModel{conn: conn, profile: profile}
}
func readySessionState() *sessionDatabase {
	p := testProfile()
	return &sessionDatabase{schema: sessionSchema{profile: &p, dim: p.Dimensions, populated: true}}
}

func TestSyncSessionPinsReadAndPublishWithoutHoldingLongTransaction(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(strconv.FormatBool(prepared), func(t *testing.T) {
			state := readySessionState()
			m := sessionModel(t, state, testProfile(), prepared)
			originalPool := m.conn.Statement.ConnPool
			ctx := context.Background()
			var escaped SyncStore
			require.NoError(t, m.WithSync(ctx, m.profile.Fingerprint, func(s SyncStore) error {
				escaped = s
				_, err := s.ListHashes(ctx)
				if err != nil {
					return err
				}
				require.NotNil(t, state.owner)
				for _, event := range state.events {
					require.False(t, event.tx, "no transaction during source/embedding preparation")
				}
				_, err = s.PublishSync(ctx, preparedBatch(1))
				return err
			}))
			require.Same(t, originalPool, m.conn.Statement.ConnPool, "do not mutate the pool model")
			require.Equal(t, prepared, m.conn.Config.PrepareStmt)
			require.Len(t, state.connections, 1)
			require.True(t, state.connections[0].closed)
			require.Nil(t, state.owner)
			for _, event := range state.events {
				require.Equal(t, 1, event.id)
			}
			_, err := escaped.ListHashes(ctx)
			require.Error(t, err, "escaped store must not acquire a new connection")
			require.Len(t, state.connections, 1)
		})
	}
}

func TestSyncSessionBusyCannotEnterCallback(t *testing.T) {
	state := readySessionState()
	a, b := sessionModel(t, state, testProfile(), false), sessionModel(t, state, testProfile(), false)
	ctx := context.Background()
	require.NoError(t, a.WithSync(ctx, a.profile.Fingerprint, func(SyncStore) error {
		returnErr := b.WithSync(ctx, b.profile.Fingerprint, func(SyncStore) error { t.Error("busy contender entered scan"); return nil })
		require.ErrorIs(t, returnErr, ErrSyncBusy)
		return nil
	}))
	require.NoError(t, b.WithSync(ctx, b.profile.Fingerprint, func(SyncStore) error { return nil }))
	require.Nil(t, state.owner)
}

func TestSyncSessionCancellationDiscardsConnectionEvenWhenWorkIgnoresContext(t *testing.T) {
	state := readySessionState()
	a, b := sessionModel(t, state, testProfile(), false), sessionModel(t, state, testProfile(), false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, finished := make(chan SyncStore, 1), make(chan struct{}), make(chan error, 1)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	go func() {
		finished <- a.WithSync(ctx, a.profile.Fingerprint, func(s SyncStore) error { entered <- s; <-release; return nil })
	}()
	var old SyncStore
	select {
	case old = <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("owner did not start")
	}
	cancel()
	require.Eventually(t, func() bool { state.mu.Lock(); defer state.mu.Unlock(); return state.owner == nil }, time.Second, time.Millisecond)
	require.NoError(t, b.WithSync(context.Background(), b.profile.Fingerprint, func(SyncStore) error { return nil }))
	_, err := old.PublishSync(context.Background(), SyncBatch{Complete: true})
	require.Error(t, err, "lost owner must not resume on another connection")
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("owner did not exit")
	}
}

func TestSyncSessionReleasesAfterErrorPanicAndLostAcquireReply(t *testing.T) {
	for _, mode := range []string{"error", "panic", "acquire reply lost"} {
		t.Run(mode, func(t *testing.T) {
			state := readySessionState()
			m := sessionModel(t, state, testProfile(), false)
			injected := errors.New("injected failure")
			if mode == "acquire reply lost" {
				state.lockErr = injected
			}
			fn := func(SyncStore) error {
				if mode == "panic" {
					panic("test-only panic")
				}
				if mode == "acquire reply lost" {
					t.Error("entered callback without lock acknowledgment")
				}
				return injected
			}
			if mode == "panic" {
				require.PanicsWithValue(t, "test-only panic", func() { _ = m.WithSync(context.Background(), m.profile.Fingerprint, fn) })
			} else {
				require.ErrorIs(t, m.WithSync(context.Background(), m.profile.Fingerprint, fn), injected)
			}
			require.Nil(t, state.owner)
			require.True(t, state.connections[0].closed)
		})
	}
}

func TestSyncSessionRequiresProfileAndOwnership(t *testing.T) {
	state := readySessionState()
	m := sessionModel(t, state, testProfile(), false)
	ctx := context.Background()
	require.ErrorIs(t, m.Upsert(ctx, nil), ErrSessionRequired)
	require.ErrorIs(t, m.UpdateMetadata(ctx, "s", `{}`), ErrSessionRequired)
	_, err := m.ListHashes(ctx)
	require.ErrorIs(t, err, ErrSessionRequired)
	_, err = m.DeleteNotIn(ctx, nil)
	require.ErrorIs(t, err, ErrSessionRequired)
	_, err = m.PublishSync(ctx, SyncBatch{Complete: true})
	require.ErrorIs(t, err, ErrSessionRequired)
	require.ErrorIs(t, m.WithSync(ctx, "wrong", func(SyncStore) error { t.Error("wrong profile admitted"); return nil }), ErrProfileMismatch)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, m.WithSync(canceled, m.profile.Fingerprint, func(SyncStore) error { return nil }), context.Canceled)
	require.Empty(t, state.connections)
	state.schema.profile = nil
	require.ErrorIs(t, m.WithSync(ctx, m.profile.Fingerprint, func(SyncStore) error { t.Error("unbound profile admitted"); return nil }), ErrProfileMismatch)
}

func TestSyncSessionConnectionLossCannotReacquirePoolConnection(t *testing.T) {
	state := readySessionState()
	m := sessionModel(t, state, testProfile(), true)
	require.Error(t, m.WithSync(context.Background(), m.profile.Fingerprint, func(s SyncStore) error {
		// Simulate a disconnected backend without canceling the application context.
		require.NoError(t, state.owner.Close())
		_, err := s.PublishSync(context.Background(), preparedBatch(1))
		return err
	}))
	require.Len(t, state.connections, 1, "lost owner cannot retry publication on a pooled connection")
	for _, event := range state.events {
		require.NotContains(t, event.sql, "INSERT ")
		require.NotContains(t, event.sql, "DELETE ")
	}
}
