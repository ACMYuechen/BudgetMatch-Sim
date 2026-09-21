package devrecords

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/memory"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type TableReport struct {
	Exists        bool  `json:"exists"`
	RowsUpTo1001  int64 `json:"rows_up_to_1001"`
	CountIsCapped bool  `json:"count_is_capped"`
}

type Report struct {
	Status            string                 `json:"status"`
	Target            Target                 `json:"target"`
	DatabaseConnected bool                   `json:"database_connected"`
	SchemaReady       bool                   `json:"schema_ready"`
	UserSelected      bool                   `json:"user_selected"`
	UserReady         bool                   `json:"user_ready"`
	Tables            map[string]TableReport `json:"tables,omitempty"`
	Demo              *DemoReport            `json:"demo,omitempty"`
	Verification      *VerificationReport    `json:"verification,omitempty"`
	Diagnostic        *Diagnostic            `json:"diagnostic,omitempty"`
	Error             string                 `json:"error,omitempty"`
}

func openDatabase(ctx context.Context, c connection, readOnly bool) (*gorm.DB, *sql.DB, error) {
	db, err := gorm.Open(postgres.Open(c.dsn(readOnly)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, nil, failure("connect", "client_setup_failed", "cannot prepare database connection; details suppressed")
	}
	pool, err := db.DB()
	if err != nil {
		return nil, nil, failure("connect", "client_setup_failed", "cannot obtain database connection pool")
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(time.Minute)
	if err := checkDatabaseIdentity(ctx, db, c.target.Database); err != nil {
		_ = pool.Close()
		return nil, nil, err
	}
	return db, pool, nil
}

func checkDatabaseIdentity(ctx context.Context, db *gorm.DB, expected string) error {
	var actual string
	if err := db.WithContext(ctx).Raw("SELECT current_database()").Scan(&actual).Error; err != nil {
		return databaseFailure("connect", err)
	}
	if actual != expected {
		return failure("connect", "database_identity_mismatch", "database identity does not match selected target; no schema changes attempted")
	}
	return nil
}

// Preflight uses a server-enforced read-only connection. Catalog/table queries
// are limited to public and bounded counts; no account list or password is read.
func preflight(ctx context.Context, db *gorm.DB, o Options, report *Report) error {
	db = db.WithContext(ctx)
	report.Tables = map[string]TableReport{}
	for _, table := range []string{"users", "products", "product_skus", "agent_conversations", "agent_conversation_turns"} {
		var exists bool
		if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname='public' AND c.relname=? AND c.relkind IN ('r','p'))`, table).Scan(&exists).Error; err != nil {
			return databaseFailure("preflight", err)
		}
		item := TableReport{Exists: exists}
		if exists {
			// table comes ONLY from the fixed list above, never from CLI/DSN input.
			if err := db.Raw(`SELECT count(*) FROM (SELECT 1 FROM public."` + table + `" LIMIT 1001) AS bounded`).Scan(&item.RowsUpTo1001).Error; err != nil {
				return databaseFailure("preflight", err)
			}
			item.CountIsCapped = item.RowsUpTo1001 == 1001
		}
		report.Tables[table] = item
	}
	store := memory.NewPostgres(db, memory.Conf{})
	report.SchemaReady = report.Tables["agent_conversations"].Exists &&
		report.Tables["agent_conversation_turns"].Exists && store.CheckSchema() == nil
	if o.UserID != "" && report.Tables["users"].Exists {
		if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM public.users WHERE id=? AND status=1 AND deleted_at IS NULL)`, o.UserID).
			Scan(&report.UserReady).Error; err != nil {
			return databaseFailure("preflight", err)
		}
	}
	return nil
}

// lockedTransactionStore is constructed ONLY after the transaction-level
// advisory lock has been obtained. Embedded methods keep the same transaction;
// using Postgres.WithConversationLock here would try to obtain another pool
// connection. The lock key matches the production user's conversation key.
type lockedTransactionStore struct {
	*memory.Postgres
	userID, conversationID string
}

func (s lockedTransactionStore) WithConversationLock(ctx context.Context, user, conversation string, fn func(context.Context) error) error {
	if user != s.userID || conversation != s.conversationID {
		return errors.New("transaction lock does not own this conversation")
	}
	return fn(ctx)
}

func persistDemo(ctx context.Context, db *gorm.DB, o Options) (*DemoReport, demoSnapshot, error) {
	var demo *DemoReport
	var snapshot demoSnapshot
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user struct{ ID string }
		// Keep the selected enabled account stable until commit. Never create an
		// account, change its role/password, or attach records to an unknown ID.
		result := tx.Raw(`SELECT id FROM public.users WHERE id=? AND status=1 AND deleted_at IS NULL FOR SHARE`, o.UserID).Scan(&user)
		if result.Error != nil || result.RowsAffected != 1 || user.ID != o.UserID {
			return errors.New("selected account is missing, disabled or unavailable")
		}
		var locked bool
		key := o.UserID + ":" + demoInputs(o)[0].ConversationId
		if tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?, 0))`, key).Scan(&locked).Error != nil || !locked {
			return errors.New("demo conversation is busy; no retry or lock takeover attempted")
		}
		store := memory.NewPostgres(tx, memory.Conf{})
		var err error
		demo, err = exercise(ctx, lockedTransactionStore{store, o.UserID, demoInputs(o)[0].ConversationId}, o)
		if err == nil {
			var found bool
			snapshot, found, err = readSnapshot(ctx, store, o)
			if err == nil && !found {
				err = errors.New("saved demo is missing before commit")
			}
		}
		return err
	})
	if err != nil {
		// A commit acknowledgement may be lost. Do not claim zero writes for all
		// driver failures; stable IDs allow a checked replay after inspection.
		return nil, demoSnapshot{}, errors.New("demo transaction did not confirm success; inspect availability and verify the same IDs (commit outcome may be unknown)")
	}
	return demo, snapshot, nil
}

// Run defaults to preflight. Write mode opens a separate connection only after
// the read-only preflight succeeds; it never migrates or repairs a schema.
func Run(ctx context.Context, o Options, environ []string) (report Report, err error) {
	report.Status = "blocked"
	defer func() {
		if err != nil {
			report.Error = err.Error() // errors above never include raw driver/config text
			report.Diagnostic = diagnosticOf(err)
		}
	}()
	c, err := loadConnection(o, environ)
	if err != nil {
		return report, err
	}
	report.Target, report.UserSelected = c.target, o.UserID != ""
	db, pool, err := openDatabase(ctx, c, true)
	if err != nil {
		return report, err
	}
	report.DatabaseConnected = true
	err = preflight(ctx, db, o, &report)
	_ = pool.Close()
	if err != nil {
		return report, err
	}
	if !o.WriteDemo && !o.VerifyDemo {
		report.Status = "preflight_only"
		return report, nil
	}
	if err := demoPrerequisites(o, report); err != nil {
		return report, err
	}
	if o.VerifyDemo {
		report.Verification, err = verifyConnection(ctx, c, o, nil)
		if err == nil {
			report.Status = "demo_verified"
		}
		return report, err
	}
	err = retainAndVerify(ctx, c, o, &report, openDatabase)
	return report, err
}

func demoPrerequisites(o Options, report Report) error {
	if !report.UserReady || !report.Tables["agent_conversations"].Exists || !report.Tables["agent_conversation_turns"].Exists {
		return errors.New("demo mode refused: existing conversation tables and enabled selected user are required; no automatic migration or account creation")
	}
	// GORM's FK check uses information_schema.table_constraints, which can
	// hide constraints from SELECT-only roles. Read verification validates the
	// actual full snapshot; only writes require the production write schema.
	if o.WriteDemo && !report.SchemaReady {
		return errors.New("write refused: existing conversation schema must pass all production checks; no automatic migration")
	}
	return nil
}

type databaseOpener func(context.Context, connection, bool) (*gorm.DB, *sql.DB, error)

// The opener is explicit to test that the writer is closed before verification
// and that a failed independent read cannot be reported as retained success.
func retainAndVerify(ctx context.Context, c connection, o Options, report *Report, open databaseOpener) error {
	db, pool, err := open(ctx, c, false)
	if err != nil {
		return err
	}
	var committed demoSnapshot
	report.Demo, committed, err = persistDemo(ctx, db, o)
	_ = pool.Close()
	if err != nil {
		return err
	}
	// A successful COMMIT is distinct from checking the retained data. Close
	// the writer before opening a new read-only pool; no cached/transaction-local
	// result can satisfy this check. Never undo a confirmed commit on failure.
	report.Status = "demo_committed_unverified"
	db, pool, err = open(ctx, c, true)
	if err == nil {
		report.Verification, err = verifyDemo(ctx, db, o, &committed)
		_ = pool.Close()
	}
	if err != nil {
		diagnostic := diagnosticOf(databaseFailure("post_commit_verify", err))
		return failure(diagnostic.Stage, diagnostic.Code, "demo commit succeeded but independent read-only verification failed; records were not removed; use -verify-demo with the same IDs")
	}
	report.Status = "demo_retained"
	return nil
}
