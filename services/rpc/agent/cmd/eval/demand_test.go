package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/eval"
	"github.com/stretchr/testify/require"
)

func TestCLIDemandReportsAndNoOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demand")
	args := []string{"-suite", "demand", "-demand-fixture", "../../testdata/eval/demand.v1.json", "-out", dir, "-revision", "local-worktree"}
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(context.Background(), args, &stdout, &stderr), stderr.String())
	var report eval.DemandReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.True(t, report.GatePassed)
	require.Len(t, report.Selection, 5)
	data, err := os.ReadFile(filepath.Join(dir, "report.json"))
	require.NoError(t, err)
	require.Equal(t, stdout.Bytes(), data)
	md, err := os.ReadFile(filepath.Join(dir, "report.md"))
	require.NoError(t, err)
	require.Equal(t, eval.DemandMarkdown(report), string(md))
	stdout.Reset()
	stderr.Reset()
	require.Equal(t, 2, run(context.Background(), args, &stdout, &stderr))
	again, err := os.ReadFile(filepath.Join(dir, "report.json"))
	require.NoError(t, err)
	require.Equal(t, data, again)
	stdout.Reset()
	stderr.Reset()
	require.Equal(t, 0, run(context.Background(), []string{"-suite", "demand", "-demand-fixture", "../../testdata/eval/demand.v1.json", "-format", "markdown"}, &stdout, &stderr))
	require.Contains(t, stdout.String(), "# Agent 需求快照选品与核验回放")
}

func TestCLIDemandRejectsMixedFlagsAndReturnsGateFailure(t *testing.T) {
	for _, args := range [][]string{
		{"-suite", "demand", "-top-k", "4"}, {"-suite", "demand", "-compare", "missing"},
		{"-suite", "demand", "-snapshot", "missing"}, {"-suite", "demand", "-cases", "missing"},
		{"-suite", "demand", "-split", "all"}, {"-suite", "demand", "-retrieval-fixture", "missing"},
		{"-demand-fixture", "missing"}, {"-suite", "scripted", "-demand-fixture", "missing"},
		{"-suite", "retrieval", "-demand-fixture", "missing"}, {"-suite", "demand", "-demand-fixture", "missing"},
	} {
		var stdout, stderr bytes.Buffer
		require.Equal(t, 2, run(context.Background(), args, &stdout, &stderr), "%v", args)
		require.Empty(t, stdout.String())
	}
	data, err := os.ReadFile("../../testdata/eval/demand.v1.json")
	require.NoError(t, err)
	var fixture eval.DemandFixture
	require.NoError(t, json.Unmarshal(data, &fixture))
	fixture.Execution[0].Expected = "no_feasible_bundle"
	data, err = json.Marshal(fixture)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "bad-expectation.json")
	require.NoError(t, os.WriteFile(file, data, 0o600))
	var stdout, stderr bytes.Buffer
	require.Equal(t, 1, run(context.Background(), []string{"-suite", "demand", "-demand-fixture", file}, &stdout, &stderr))
	var report eval.DemandReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.False(t, report.GatePassed)
	require.False(t, report.Execution[0].OutcomeMatched)
	stdout.Reset()
	stderr.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Equal(t, 2, run(ctx, []string{"-suite", "demand", "-demand-fixture", "../../testdata/eval/demand.v1.json"}, &stdout, &stderr))
	require.Empty(t, stdout.String())
}
