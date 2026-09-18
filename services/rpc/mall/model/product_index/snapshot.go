package product_index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"budgetmatch-sim/services/rpc/mall/indexcontract"

	"gorm.io/gorm"
)

var (
	ErrSnapshotBusy  = errors.New("product index snapshot busy")
	ErrSnapshotLimit = errors.New("product index snapshot limit exceeded")
)

// SnapshotReader invokes visit sequentially with data pages from one database
// snapshot. Nil means all rows were visited AND the read transaction committed.
// Callbacks must honor ctx; in production the gRPC transport has this deadline.
type SnapshotReader interface {
	ScanSnapshot(ctx context.Context, pageSize int, visit func([]Entry) error) error
}

// ValidateSnapshotContext requires a transport deadline. A derived context alone
// cannot unblock a gRPC Send on the original stream; reject unbounded callers.
func ValidateSnapshotContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > indexcontract.MaxDuration {
		return fmt.Errorf("product index snapshot requires a deadline within %s", indexcontract.MaxDuration)
	}
	return nil
}

func (m *Model) ScanSnapshot(ctx context.Context, pageSize int, visit func([]Entry) error) error {
	if err := ValidateSnapshotContext(ctx); err != nil {
		return err
	}
	if pageSize < 1 || pageSize > MaxPageSize || visit == nil {
		return fmt.Errorf("invalid product index snapshot bounds")
	}
	if m.conn == nil {
		return fmt.Errorf("product index snapshot database is required")
	}
	// GORM nests Transaction with a savepoint, which cannot upgrade an existing
	// READ COMMITTED transaction to REPEATABLE READ or make it read-only.
	if _, nested := m.conn.Statement.ConnPool.(gorm.TxCommitter); nested {
		return fmt.Errorf("product index snapshot requires its own transaction")
	}
	// One in-flight export per model/service instance. This is resource admission,
	// not a distributed lock; separate Mall replicas can each serve a snapshot.
	if !m.snapshotMu.TryLock() {
		return ErrSnapshotBusy
	}
	defer m.snapshotMu.Unlock()
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reader := &Model{conn: tx}
		cursor, count := "", 0
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			rows, err := reader.ListPage(ctx, cursor, pageSize)
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(rows) > pageSize {
				return fmt.Errorf("oversized product index page")
			}
			if len(rows) > indexcontract.MaxItems-count {
				return ErrSnapshotLimit
			}
			for _, row := range rows {
				if row.SkuId == "" || !ValidCursor(row.SkuId) || row.SkuId <= cursor {
					return fmt.Errorf("non-progressing product index snapshot")
				}
				cursor = row.SkuId
			}
			if len(rows) > 0 {
				if err := visit(rows); err != nil {
					return err
				}
				count += len(rows)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(rows) < pageSize {
				return nil
			}
		}
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}
