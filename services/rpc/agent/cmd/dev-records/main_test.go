package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/devrecords"

	"github.com/stretchr/testify/require"
)

func baseArgs() []string {
	return []string{"-env", "unused", "-expect-db", "dev_records", "-allow-local-dev-db"}
}

func TestCLIRefusesMissingGrantAmbiguousSourceAndMissingWriteIdentityBeforeExecution(t *testing.T) {
	for _, args := range [][]string{nil, {"-env", "unused", "-expect-db", "dev_records"},
		append(baseArgs(), "-config", "second"), append(baseArgs(), "-write-demo"), append(baseArgs(), "extra"),
		append(baseArgs(), "-verify-demo"),
		append(baseArgs(), "-verify-demo", "-write-demo", "-user-id", "demo-user", "-run-id", "demo-001")} {
		var out, stderr bytes.Buffer
		called := false
		code := run(args, &out, &stderr, func(context.Context, devrecords.Options, []string) (devrecords.Report, error) {
			called = true
			return devrecords.Report{}, nil
		})
		require.Equal(t, 2, code)
		require.False(t, called)
	}
}

func TestCLIReadOnlyDefaultAndPrivateReportCannotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	args := append(baseArgs(), "-report", path)
	var out, stderr bytes.Buffer
	calls := 0
	exec := func(ctx context.Context, o devrecords.Options, _ []string) (devrecords.Report, error) {
		calls++
		require.False(t, o.WriteDemo)
		require.False(t, o.VerifyDemo)
		_, ok := ctx.Deadline()
		require.True(t, ok)
		return devrecords.Report{Status: "preflight_only"}, nil
	}
	require.Zero(t, run(args, &out, &stderr, exec))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "preflight_only")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0077)
	require.Equal(t, 2, run(args, &out, &stderr, exec))
	require.Equal(t, 1, calls)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, after)
}

func TestCLIExecutionFailureIsNotReportedAsSuccessfulPreflight(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run(baseArgs(), &out, &stderr, func(context.Context, devrecords.Options, []string) (devrecords.Report, error) {
		return devrecords.Report{Status: "blocked", Error: "database unavailable"}, errors.New("database unavailable")
	})
	require.Equal(t, 1, code)
	require.Contains(t, out.String(), "blocked")
}

func TestCLIVerifyModeNeverSelectsWrites(t *testing.T) {
	var out, stderr bytes.Buffer
	args := append(baseArgs(), "-verify-demo", "-user-id", "demo-user", "-run-id", "demo-001")
	called := false
	code := run(args, &out, &stderr, func(_ context.Context, o devrecords.Options, _ []string) (devrecords.Report, error) {
		called = true
		require.True(t, o.VerifyDemo)
		require.False(t, o.WriteDemo)
		require.Equal(t, "demo-user", o.UserID)
		require.Equal(t, "demo-001", o.RunID)
		return devrecords.Report{Status: "demo_verified"}, nil
	})
	require.True(t, called)
	require.Zero(t, code)
	require.Contains(t, out.String(), "demo_verified")
}

func TestCLIConfirmedCommitWithFailedVerificationIsNotSuccess(t *testing.T) {
	var out, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "committed-unverified.json")
	args := append(baseArgs(), "-write-demo", "-user-id", "demo-user", "-run-id", "demo-001", "-report", path)
	code := run(args, &out, &stderr, func(context.Context, devrecords.Options, []string) (devrecords.Report, error) {
		return devrecords.Report{Status: "demo_committed_unverified", Demo: &devrecords.DemoReport{Created: true}}, errors.New("verification unavailable")
	})
	require.Equal(t, 1, code)
	require.Contains(t, out.String(), "demo_committed_unverified")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, out.String(), string(data))
}

func TestCLIRemoteModePinsTargetAndDefaultsToReadOnly(t *testing.T) {
	args := []string{"-config", "unused-private.yaml", "-expect-db", "dev_records", "-allow-remote-dev-db", "-expect-address", "192.0.2.10:5432"}
	var out, stderr bytes.Buffer
	called := false
	code := run(args, &out, &stderr, func(_ context.Context, o devrecords.Options, _ []string) (devrecords.Report, error) {
		called = true
		require.True(t, o.AllowRemote)
		require.False(t, o.AllowLocal)
		require.False(t, o.WriteDemo)
		require.False(t, o.VerifyDemo)
		require.Equal(t, "192.0.2.10:5432", o.ExpectedAddress)
		require.Equal(t, "unused-private.yaml", o.ConfigFile)
		return devrecords.Report{Status: "preflight_only"}, nil
	})
	require.True(t, called)
	require.Zero(t, code)
	for _, invalid := range [][]string{
		append(append([]string{}, args...), "-allow-local-dev-db"),
		append(append([]string{}, args...), "-env", ".env"),
		append(append([]string{}, args...), "-expect-address", "127.0.0.1:5432"),
		append(append([]string{}, args...), "-expect-address", ""),
		append(append([]string{}, args...), "-write-demo"),
	} {
		called = false
		code = run(invalid, &out, &stderr, func(context.Context, devrecords.Options, []string) (devrecords.Report, error) {
			called = true
			return devrecords.Report{}, nil
		})
		require.False(t, called)
		require.Equal(t, 2, code)
	}
}
