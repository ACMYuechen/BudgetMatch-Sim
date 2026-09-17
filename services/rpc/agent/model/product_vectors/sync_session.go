package product_vectors

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var (
	ErrSyncBusy        = errors.New("product_vectors: index synchronization busy")
	ErrSessionRequired = errors.New("product_vectors: owned synchronization session required")
)

// Fixed across model versions, processes and schemas in the same database. Using
// a model-specific key would let different models overwrite the same table.
const syncLockKey int64 = 0x42554d4154564543 // BUMATVEC

// withSession reserves ONE physical connection, not a transaction, while external
// work runs. Every query and the eventual transaction use this same connection.
// On completion/cancellation it is discarded, not returned to the idle pool with
// a session lock. Connection loss therefore cannot turn into an unlocked writer.
// Requires a direct PostgreSQL connection, or session pooling that preserves
// session identity and resets advisory locks on client disconnect. Transaction
// pooling is unsupported.
func (m *defaultProductVectorsModel) withSession(ctx context.Context, fn func(*defaultProductVectorsModel) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.ownedSession {
		return fmt.Errorf("product_vectors: nested synchronization session")
	}
	db, err := m.conn.DB()
	if err != nil {
		return fmt.Errorf("product_vectors: get pool: %w", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("product_vectors: reserve connection: %w", err)
	}
	discard := func() {
		// Raw's ErrBadConn invalidates sql.Conn and physically closes its driver
		// connection. Close alone would return it to the pool, retaining the lock.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}
	stop := context.AfterFunc(ctx, discard)
	defer func() { stop(); discard() }()
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", syncLockKey).Scan(&acquired); err != nil {
		return fmt.Errorf("product_vectors: acquire synchronization session: %w", err)
	}
	if !acquired {
		return ErrSyncBusy
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bound := m.conn.Session(&gorm.Session{NewDB: true, Context: ctx})
	bound.Statement.ConnPool = conn
	bound.Config.ConnPool = conn
	bound.Config.PrepareStmt = false // Do not route through pool-level prepared statements.
	return fn(&defaultProductVectorsModel{conn: bound, profile: m.profile, ownedSession: true})
}

// WithSync must be entered before loading the catalog; locking only publication
// would serialize stale scans without preventing the older one publishing last.
func (m *defaultProductVectorsModel) WithSync(ctx context.Context, fingerprint string, fn func(SyncStore) error) error {
	if err := m.profile.Validate(); err != nil {
		return err
	}
	if fingerprint != m.profile.Fingerprint {
		return ErrProfileMismatch
	}
	if fn == nil {
		return fmt.Errorf("product_vectors: synchronization callback is required")
	}
	return m.withSession(ctx, func(session *defaultProductVectorsModel) error {
		if err := session.checkProfile(ctx); err != nil {
			return err
		}
		return fn(session)
	})
}
