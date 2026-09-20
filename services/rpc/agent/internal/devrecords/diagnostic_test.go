package devrecords

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const privateErrorMarker = "synthetic-password-and-private-query"

// Implements pgx's typed SQLState contract without a new direct dependency.
type stateError string

func (e stateError) Error() string    { return privateErrorMarker }
func (e stateError) SQLState() string { return string(e) }

func TestDatabaseDiagnosticClassifiesTypedCausesAndDropsPrivateDetails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"bad password", stateError("28P01"), "authentication_failed"},
		{"authentication rejected", stateError("28000"), "authentication_failed"},
		{"database missing", stateError("3D000"), "database_not_found"},
		{"SQL privilege", stateError("42501"), "database_permission_denied"},
		{"connection limit", stateError("53300"), "database_unavailable"},
		{"starting server", stateError("57P03"), "database_unavailable"},
		{"statement canceled", stateError("57014"), "query_canceled"},
		{"unknown SQLSTATE", stateError("XX000"), "database_error"},
		{"untrusted SQLSTATE", stateError(privateErrorMarker), "database_error"},
		{"socket permission", os.NewSyscallError("connect", syscall.EACCES), "network_permission_denied"},
		{"operation permission", syscall.EPERM, "network_permission_denied"},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, "connection_refused"},
		{"network unreachable", syscall.ENETUNREACH, "network_unreachable"},
		{"host unreachable", syscall.EHOSTUNREACH, "network_unreachable"},
		{"timeout", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ETIMEDOUT}, "operation_timeout"},
		{"deadline", context.DeadlineExceeded, "operation_timeout"},
		{"canceled", context.Canceled, "operation_canceled"},
		{"private string", errors.New(privateErrorMarker), "database_error"},
		{"no text matching", errors.New("SQLSTATE 42501 permission denied connection refused password failed"), "database_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Errorf("%s: %w", privateErrorMarker, tc.err)
			safe := databaseFailure("connect", raw)
			diagnostic := diagnosticOf(safe)
			require.Equal(t, &Diagnostic{Stage: "connect", Code: tc.code}, diagnostic)
			require.NotContains(t, safe.Error(), privateErrorMarker)
			require.NotContains(t, fmt.Sprintf("%+v", safe), privateErrorMarker)
			require.Nil(t, errors.Unwrap(safe))
			data, err := json.Marshal(Report{Status: "blocked", Error: safe.Error(), Diagnostic: diagnostic})
			require.NoError(t, err)
			require.NotContains(t, string(data), privateErrorMarker)
			require.Contains(t, string(data), tc.code)
		})
	}
}

func TestDiagnosticCopyAndPhasePropagation(t *testing.T) {
	first := databaseFailure("connect", stateError("42501"))
	copy := diagnosticOf(first)
	copy.Code = privateErrorMarker
	require.Equal(t, "database_permission_denied", diagnosticOf(first).Code)
	second := databaseFailure("post_commit_verify", first)
	require.Equal(t, &Diagnostic{Stage: "post_commit_verify", Code: "database_permission_denied"}, diagnosticOf(second))
	require.Nil(t, diagnosticOf(errors.New("ordinary error")))
}

func TestDatabaseIdentityDistinguishesMismatchFromAccessFailure(t *testing.T) {
	db, driver := replayDatabase(t)
	require.NoError(t, checkDatabaseIdentity(t.Context(), db, "dev_records"))
	err := checkDatabaseIdentity(t.Context(), db, "other_database")
	require.Equal(t, &Diagnostic{Stage: "connect", Code: "database_identity_mismatch"}, diagnosticOf(err))
	require.NotContains(t, err.Error(), "other_database")
	driver.queryError = fmt.Errorf("%s: %w", privateErrorMarker, stateError("28P01"))
	err = checkDatabaseIdentity(t.Context(), db, "dev_records")
	require.Equal(t, &Diagnostic{Stage: "connect", Code: "authentication_failed"}, diagnosticOf(err))
	require.NotContains(t, err.Error(), privateErrorMarker)
}

func TestReadOnlyPreflightReportsDatabasePermissionWithoutLeakingQuery(t *testing.T) {
	db, driver := replayDatabase(t)
	driver.queryError = stateError("42501")
	var report Report
	err := preflight(t.Context(), db, validOptions(), &report)
	require.Equal(t, &Diagnostic{Stage: "preflight", Code: "database_permission_denied"}, diagnosticOf(err))
	require.NotContains(t, err.Error(), privateErrorMarker)
	require.False(t, report.SchemaReady)
	require.Zero(t, driver.begins)
}

func TestReadOnlyVerificationKeepsSafeCause(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*replayDriver)
		code string
	}{
		{"permission", func(d *replayDriver) { d.queryError = stateError("42501") }, "database_permission_denied"},
		{"missing", func(d *replayDriver) { d.missing = true }, "demo_missing"},
		{"edited", func(d *replayDriver) { d.snapshot.Conversation.Title = "changed" }, "demo_mismatch"},
		{"account", func(d *replayDriver) { d.userPresent = false }, "selected_user_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, driver := replayDatabase(t)
			tc.edit(driver)
			_, err := verifyDemo(t.Context(), db, validOptions(), nil)
			require.ErrorContains(t, err, "read-only demo verification failed")
			require.Equal(t, &Diagnostic{Stage: "verify", Code: tc.code}, diagnosticOf(err))
			require.NotContains(t, err.Error(), privateErrorMarker)
		})
	}
}

// This is a bounded local wire-protocol double, not a real PostgreSQL instance.
// It verifies the actual GORM/pgx error wrapping and Run report without trying
// wrong passwords, missing databases or permission faults on the user's DB.
func TestRunClassifiesPostgresStartupResponsesThroughRealDriver(t *testing.T) {
	for _, tc := range []struct{ state, code string }{
		{"28P01", "authentication_failed"}, {"28000", "authentication_failed"},
		{"3D000", "database_not_found"}, {"42501", "database_permission_denied"},
		{"57P03", "database_unavailable"}, {"XX000", "database_error"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			finished := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					finished <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				var header [4]byte
				if _, err := io.ReadFull(conn, header[:]); err != nil {
					finished <- err
					return
				}
				size := binary.BigEndian.Uint32(header[:])
				if size < 8 || size > 16384 {
					finished <- errors.New("unexpected startup length")
					return
				}
				startup := make([]byte, size-4)
				if _, err := io.ReadFull(conn, startup); err != nil {
					finished <- err
					return
				}
				if binary.BigEndian.Uint32(startup[:4]) != 196608 {
					finished <- errors.New("expected PostgreSQL v3 startup")
					return
				}
				body := []byte("SFATAL\x00C" + tc.state + "\x00M" + privateErrorMarker + "\x00D" + privateErrorMarker + "\x00\x00")
				frame := make([]byte, 5+len(body))
				frame[0] = 'E'
				binary.BigEndian.PutUint32(frame[1:5], uint32(4+len(body)))
				copy(frame[5:], body)
				_, err = conn.Write(frame)
				finished <- err
			}()
			_, port, err := net.SplitHostPort(listener.Addr().String())
			require.NoError(t, err)
			o := validOptions()
			o.WriteDemo, o.RunID, o.UserID = false, "", ""
			o.EnvFile = filepath.Join(t.TempDir(), "synthetic.env")
			dsn := strings.Replace(testDSN, "port=15432", "port="+port, 1)
			require.NoError(t, os.WriteFile(o.EnvFile, []byte(o.DSNKey+"="+dsn+"\n"), 0600))
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			report, err := Run(ctx, o, os.Environ())
			require.Error(t, err)
			require.Equal(t, "blocked", report.Status)
			require.Equal(t, &Diagnostic{Stage: "connect", Code: tc.code}, report.Diagnostic)
			require.False(t, report.DatabaseConnected)
			require.Nil(t, report.Tables)
			require.Nil(t, report.Demo)
			data, err := json.Marshal(report)
			require.NoError(t, err)
			require.NotContains(t, string(data), privateErrorMarker)
			require.NotContains(t, string(data), "synthetic-secret")
			select {
			case err := <-finished:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("wire double did not finish within deadline")
			}
		})
	}
}
