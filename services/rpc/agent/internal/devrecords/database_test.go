package devrecords

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This is a protocol/transaction ordering double, not PostgreSQL. It returns
// an already retained fixture and rejects every mutation/DDL statement.
type replayDriver struct {
	snapshot                               demoSnapshot
	userPresent, lockAvailable, failCommit bool
	missing, failBegin, failQuery          bool
	queries                                []string
	begins, commits, rollbacks             int
	options                                []driver.TxOptions
	closes                                 int
}

func (d *replayDriver) Connect(context.Context) (driver.Conn, error) { return &replayConn{d}, nil }
func (d *replayDriver) Driver() driver.Driver                        { return d }
func (d *replayDriver) Open(string) (driver.Conn, error)             { return &replayConn{d}, nil }

type replayConn struct{ d *replayDriver }

func (*replayConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (c *replayConn) Close() error                      { c.d.closes++; return nil }
func (c *replayConn) Begin() (driver.Tx, error)         { c.d.begins++; return &replayTx{c.d}, nil }
func (c *replayConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.d.options = append(c.d.options, options)
	if c.d.failBegin {
		return nil, errors.New("synthetic private driver begin error")
	}
	return c.Begin()
}
func (c *replayConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.d.queries = append(c.d.queries, query)
	return nil, errors.New("mutations are forbidden during retained replay")
}
func (c *replayConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.d.queries = append(c.d.queries, query)
	if c.d.failQuery {
		return nil, errors.New("synthetic private driver query error")
	}
	if !strings.HasPrefix(query, "SELECT ") {
		return nil, errors.New("unexpected mutation")
	}
	s := c.d.snapshot
	switch {
	case strings.Contains(query, "FROM public.users"):
		if strings.Contains(query, "SELECT EXISTS") {
			return &replayRows{columns: []string{"exists"}, values: [][]driver.Value{{c.d.userPresent}}}, nil
		}
		r := &replayRows{columns: []string{"id"}}
		if c.d.userPresent {
			r.values = [][]driver.Value{{s.Conversation.UserId}}
		}
		return r, nil
	case strings.Contains(query, "pg_try_advisory_xact_lock"):
		return &replayRows{columns: []string{"locked"}, values: [][]driver.Value{{c.d.lockAvailable}}}, nil
	case strings.Contains(query, `FROM "agent_conversations"`):
		state, _ := json.Marshal(s.Conversation.State)
		v := s.Conversation
		r := &replayRows{columns: []string{"user_id", "conversation_id", "title", "state", "version", "turn_count", "created_at", "updated_at"}}
		if !c.d.missing && args[0].Value == v.UserId && args[1].Value == v.ConversationId {
			r.values = [][]driver.Value{{v.UserId, v.ConversationId, v.Title, state, v.Version, v.TurnCount, v.CreatedAt, v.UpdatedAt}}
		}
		return r, nil
	case strings.Contains(query, `count(*) FROM "agent_conversation_turns"`):
		return &replayRows{columns: []string{"count"}, values: [][]driver.Value{{int64(len(s.Turns))}}}, nil
	case strings.Contains(query, `FROM "agent_conversation_turns"`):
		r := &replayRows{columns: []string{"user_id", "conversation_id", "turn_id", "sequence", "status", "query", "budget_cents", "max_items", "intent", "result", "summary", "created_at", "completed_at"}}
		for _, v := range s.Turns {
			if strings.Contains(query, "turn_id =") && args[2].Value != v.TurnId {
				continue
			}
			intent, _ := json.Marshal(v.Intent)
			r.values = append(r.values, []driver.Value{v.UserId, v.ConversationId, v.TurnId, v.Sequence, v.Status, v.Query,
				v.BudgetCents, int64(v.MaxItems), intent, []byte(v.ResultJSON), v.Summary, v.CreatedAt, v.CompletedAt})
		}
		return r, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

type replayTx struct{ d *replayDriver }

func (t *replayTx) Commit() error {
	t.d.commits++
	if t.d.failCommit {
		return errors.New("synthetic private driver text must not escape")
	}
	return nil
}
func (t *replayTx) Rollback() error { t.d.rollbacks++; return nil }

type replayRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *replayRows) Columns() []string { return r.columns }
func (*replayRows) Close() error        { return nil }
func (r *replayRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}

func replayDatabase(t *testing.T) (*gorm.DB, *replayDriver) {
	t.Helper()
	s, err := expectedSnapshot(t.Context(), validOptions())
	require.NoError(t, err)
	d := &replayDriver{snapshot: s, userPresent: true, lockAvailable: true}
	pool := sql.OpenDB(d)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	require.NoError(t, err)
	return db, d
}

func TestDatabaseReplayUsesOneTransactionAndNeverMutates(t *testing.T) {
	db, d := replayDatabase(t)
	report, snapshot, err := persistDemo(t.Context(), db, validOptions())
	require.NoError(t, err)
	require.False(t, report.Created)
	require.Zero(t, report.ProviderCalls)
	require.True(t, report.ReplayVerified)
	require.True(t, sameSnapshot(d.snapshot, snapshot, false))
	require.Equal(t, 1, d.begins)
	require.Equal(t, 1, d.commits)
	require.Zero(t, d.rollbacks)
	require.Contains(t, d.queries[0], "FOR SHARE")
	require.Contains(t, d.queries[1], "pg_try_advisory_xact_lock")
	for _, query := range d.queries {
		require.True(t, strings.HasPrefix(query, "SELECT "), query)
	}
}

func TestDatabaseGuardsRollbackWithoutRepairsAndCommitUncertaintyIsExplicit(t *testing.T) {
	for _, scenario := range []string{"user missing", "lock busy", "foreign data", "commit lost"} {
		t.Run(scenario, func(t *testing.T) {
			db, d := replayDatabase(t)
			switch scenario {
			case "user missing":
				d.userPresent = false
			case "lock busy":
				d.lockAvailable = false
			case "foreign data":
				d.snapshot.Conversation.Title = "foreign"
			case "commit lost":
				d.failCommit = true
			}
			report, _, err := persistDemo(t.Context(), db, validOptions())
			require.ErrorContains(t, err, "commit outcome may be unknown")
			require.NotContains(t, err.Error(), "private driver")
			require.Nil(t, report)
			if scenario != "commit lost" {
				require.Equal(t, 1, d.rollbacks)
				require.Zero(t, d.commits)
			}
			for _, query := range d.queries {
				require.True(t, strings.HasPrefix(query, "SELECT "), query)
			}
		})
	}
}

func TestTransactionStoreCannotReuseLockForAnotherNamespace(t *testing.T) {
	store := lockedTransactionStore{userID: "u", conversationID: "c"}
	called := false
	err := store.WithConversationLock(t.Context(), "other", "c", func(context.Context) error { called = true; return nil })
	require.Error(t, err)
	require.False(t, called)
}
