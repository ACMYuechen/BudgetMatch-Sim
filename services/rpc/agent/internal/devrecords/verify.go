package devrecords

import (
	"context"
	"database/sql"
	"errors"

	"budgetmatch-sim/services/rpc/agent/internal/memory"

	"gorm.io/gorm"
)

// VerificationReport describes one consistent database snapshot, not ongoing
// availability, browser visibility, model quality or restart/crash durability.
type VerificationReport struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Turns          int    `json:"turns"`
	ComparedWith   string `json:"compared_with"`
}

func verifyConnection(ctx context.Context, c connection, o Options, committed *demoSnapshot) (*VerificationReport, error) {
	db, pool, err := openDatabase(ctx, c, true)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return verifyDemo(ctx, db, o, committed)
}

// Only SELECTs run against the selected database. A repeatable-read, read-only
// transaction keeps ListTurns' metadata/count/page and History consistent even
// if a service changes this conversation concurrently. No conversation/account
// locks, service execution, SaveTurn, migration or repair is performed here.
func verifyDemo(ctx context.Context, db *gorm.DB, o Options, committed *demoSnapshot) (*VerificationReport, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if !o.WriteDemo && !o.VerifyDemo {
		return nil, errors.New("demo verification is not selected")
	}
	expected, err := expectedSnapshot(ctx, o)
	if err != nil {
		return nil, errors.New("cannot prepare deterministic demo fixture for verification")
	}
	comparedWith := "versioned_fixture"
	if committed != nil {
		comparedWith = "committed_snapshot"
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var enabled bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM public.users WHERE id=? AND status=1 AND deleted_at IS NULL)`, o.UserID).
			Scan(&enabled).Error; err != nil {
			return databaseFailure("verify", err)
		}
		if !enabled {
			return failure("verify", "selected_user_unavailable", "selected account is missing or disabled")
		}
		actual, found, err := readSnapshot(ctx, memory.NewPostgres(tx, memory.Conf{}), o)
		if err != nil {
			return databaseFailure("verify", err)
		}
		if !found {
			return failure("verify", "demo_missing", "retained demo is missing; no records created")
		}
		if !sameSnapshot(actual, expected, true) {
			return failure("verify", "demo_mismatch", "retained demo contains different or partial data; no repairs attempted")
		}
		if committed != nil && !sameSnapshot(actual, *committed, false) {
			return failure("verify", "committed_snapshot_mismatch", "retained demo differs from the confirmed transaction snapshot")
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		// Driver Begin/Commit errors can include connection details; keep them
		// private just like query errors. Missing/mismatched data is not a pass.
		diagnostic := diagnosticOf(databaseFailure("verify", err))
		return nil, failure(diagnostic.Stage, diagnostic.Code, "read-only demo verification failed; check availability, selected account and complete fixture; no records changed")
	}
	return &VerificationReport{UserID: o.UserID, ConversationID: demoInputs(o)[0].ConversationId,
		Turns: len(expected.Turns), ComparedWith: comparedWith}, nil
}
