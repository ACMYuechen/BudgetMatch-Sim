package devrecords

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVerifyUsesOneReadOnlyConsistentSnapshotWithoutServiceExecution(t *testing.T) {
	for _, compareCommit := range []bool{false, true} {
		db, d := replayDatabase(t)
		o := validOptions()
		o.WriteDemo, o.VerifyDemo = false, true
		var committed *demoSnapshot
		comparison := "versioned_fixture"
		if compareCommit {
			committed, comparison = &d.snapshot, "committed_snapshot"
		}
		report, err := verifyDemo(t.Context(), db, o, committed)
		require.NoError(t, err)
		require.Equal(t, o.UserID, report.UserID)
		require.Equal(t, demoInputs(o)[0].ConversationId, report.ConversationID)
		require.Equal(t, 2, report.Turns)
		require.Equal(t, comparison, report.ComparedWith)
		require.Equal(t, []driver.TxOptions{{ReadOnly: true, Isolation: driver.IsolationLevel(sql.LevelRepeatableRead)}}, d.options)
		require.Equal(t, 1, d.commits)
		require.Zero(t, d.rollbacks)
		for _, query := range d.queries {
			require.True(t, strings.HasPrefix(query, "SELECT "), query)
			require.NotContains(t, query, "FOR SHARE")
			require.NotContains(t, query, "advisory")
			require.NotContains(t, query, "turn_id =") // no Service replay / FindTurn
		}
	}
}

func TestVerifyRefusesMissingPartialForeignDataAndSanitizesDriverFailures(t *testing.T) {
	for _, scenario := range []string{"missing", "partial", "edited", "other user", "other run", "account unavailable", "begin failed", "query failed", "commit failed", "timestamp changed"} {
		t.Run(scenario, func(t *testing.T) {
			db, d := replayDatabase(t)
			o := validOptions()
			o.WriteDemo, o.VerifyDemo = false, true
			committed := d.snapshot
			switch scenario {
			case "missing":
				d.missing = true
			case "partial":
				d.snapshot.Turns = d.snapshot.Turns[:1]
			case "edited":
				d.snapshot.Conversation.Title = "other data"
			case "other user":
				o.UserID = "other-user"
			case "other run":
				o.RunID = "other-run"
			case "account unavailable":
				d.userPresent = false
			case "begin failed":
				d.failBegin = true
			case "query failed":
				d.failQuery = true
			case "commit failed":
				d.failCommit = true
			case "timestamp changed":
				d.snapshot.Conversation.UpdatedAt = d.snapshot.Conversation.UpdatedAt.Add(time.Microsecond)
			}
			report, err := verifyDemo(t.Context(), db, o, &committed)
			require.ErrorContains(t, err, "read-only demo verification failed")
			require.NotContains(t, err.Error(), "private driver")
			require.Nil(t, report)
			for _, query := range d.queries {
				require.True(t, strings.HasPrefix(query, "SELECT "), query)
			}
		})
	}
}

func TestStandaloneVerificationDoesNotInventAnOriginalCommitTimestamp(t *testing.T) {
	db, d := replayDatabase(t)
	d.snapshot.Conversation.UpdatedAt = d.snapshot.Conversation.UpdatedAt.Add(time.Hour)
	o := validOptions()
	o.WriteDemo, o.VerifyDemo = false, true
	report, err := verifyDemo(t.Context(), db, o, nil)
	require.NoError(t, err)
	require.Equal(t, "versioned_fixture", report.ComparedWith)
}

func TestVerifyRejectsUnselectedOrCanceledChecksBeforeDatabaseAccess(t *testing.T) {
	db, d := replayDatabase(t)
	o := validOptions()
	o.WriteDemo, o.RunID = false, ""
	_, err := verifyDemo(t.Context(), db, o, nil)
	require.ErrorContains(t, err, "not selected")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = verifyDemo(ctx, db, validOptions(), nil)
	require.Error(t, err)
	require.Empty(t, d.queries)
	require.Zero(t, d.begins)
}

func TestReadVerificationDoesNotRequireWriteSchemaButCannotRelaxWrites(t *testing.T) {
	for _, mode := range []string{"write", "verify"} {
		for _, missing := range []string{"none", "write schema", "user", "conversation table", "turn table"} {
			t.Run(mode+"/"+missing, func(t *testing.T) {
				o := validOptions()
				o.WriteDemo, o.VerifyDemo = mode == "write", mode == "verify"
				r := Report{UserReady: true, SchemaReady: true, Tables: map[string]TableReport{
					"agent_conversations": {Exists: true}, "agent_conversation_turns": {Exists: true},
				}}
				switch missing {
				case "write schema":
					r.SchemaReady = false
				case "user":
					r.UserReady = false
				case "conversation table":
					delete(r.Tables, "agent_conversations")
				case "turn table":
					delete(r.Tables, "agent_conversation_turns")
				}
				err := demoPrerequisites(o, r)
				if missing == "none" || missing == "write schema" && mode == "verify" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestRetainClosesWriterBeforeIndependentVerificationAndKeepsCommitOutcome(t *testing.T) {
	for _, scenario := range []string{"success", "commit unknown", "reader unavailable", "changed snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			writer, writes := replayDatabase(t)
			reader, reads := replayDatabase(t)
			reads.snapshot = writes.snapshot
			if scenario == "commit unknown" {
				writes.failCommit = true
			}
			if scenario == "changed snapshot" {
				reads.snapshot.Conversation.UpdatedAt = reads.snapshot.Conversation.UpdatedAt.Add(time.Microsecond)
			}
			var modes []bool
			open := func(_ context.Context, _ connection, readOnly bool) (*gorm.DB, *sql.DB, error) {
				modes = append(modes, readOnly)
				db := writer
				if readOnly {
					require.Equal(t, 1, writes.commits)
					require.Equal(t, 1, writes.closes)
					if scenario == "reader unavailable" {
						return nil, nil, databaseFailure("connect", syscall.ECONNREFUSED)
					}
					db = reader
				}
				pool, err := db.DB()
				return db, pool, err
			}
			report := Report{Status: "blocked"}
			err := retainAndVerify(t.Context(), connection{}, validOptions(), &report, open)
			if scenario == "commit unknown" {
				require.ErrorContains(t, err, "commit outcome may be unknown")
				require.Equal(t, []bool{false}, modes)
				require.Equal(t, "blocked", report.Status)
				require.Nil(t, report.Demo)
				require.Nil(t, report.Verification)
				return
			}
			require.Equal(t, []bool{false, true}, modes)
			require.NotNil(t, report.Demo)
			if scenario == "success" {
				require.NoError(t, err)
				require.Equal(t, "demo_retained", report.Status)
				require.Equal(t, "committed_snapshot", report.Verification.ComparedWith)
			} else {
				require.ErrorContains(t, err, "commit succeeded")
				require.NotContains(t, err.Error(), "private driver")
				require.Equal(t, "demo_committed_unverified", report.Status)
				require.Nil(t, report.Verification)
				if scenario == "reader unavailable" {
					require.Equal(t, &Diagnostic{Stage: "post_commit_verify", Code: "connection_refused"}, diagnosticOf(err))
				}
			}
		})
	}
}
