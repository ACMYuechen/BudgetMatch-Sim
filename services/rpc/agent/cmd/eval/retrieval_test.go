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

func TestCLIRetrievalReportAndExclusiveArtifacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "retrieval")
	args := []string{"-suite", "retrieval", "-retrieval-fixture", "../../testdata/eval/retrieval.v1.json", "-out", dir, "-revision", "local-worktree"}
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(context.Background(), args, &stdout, &stderr), stderr.String())
	var report eval.RetrievalReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.True(t, report.GatePassed)
	require.Len(t, report.Strategies, 4)
	data, err := os.ReadFile(filepath.Join(dir, "report.json"))
	require.NoError(t, err)
	require.Equal(t, stdout.Bytes(), data)
	md, err := os.ReadFile(filepath.Join(dir, "report.md"))
	require.NoError(t, err)
	require.Equal(t, eval.RetrievalMarkdown(report), string(md))
	stdout.Reset()
	stderr.Reset()
	require.Equal(t, 2, run(context.Background(), args, &stdout, &stderr))
	again, err := os.ReadFile(filepath.Join(dir, "report.json"))
	require.NoError(t, err)
	require.Equal(t, data, again)
	stdout.Reset()
	stderr.Reset()
	require.Equal(t, 0, run(context.Background(), []string{"-suite", "retrieval", "-retrieval-fixture", "../../testdata/eval/retrieval.v1.json", "-format", "markdown"}, &stdout, &stderr))
	require.Contains(t, stdout.String(), "# Agent 检索排序回放对比")
}

func TestCLIRetrievalRejectsMixedParametersAndDoesNotHideGateFailures(t *testing.T) {
	for _, args := range [][]string{
		{"-suite", "retrieval", "-top-k", "4"}, {"-suite", "retrieval", "-compare", "missing"},
		{"-suite", "retrieval", "-split", "all"}, {"-suite", "retrieval", "-snapshot", "missing"},
		{"-suite", "retrieval", "-cases", "missing"}, {"-retrieval-fixture", "missing"},
		{"-suite", "scripted", "-retrieval-fixture", "missing"}, {"-suite", "retrieval", "-retrieval-fixture", "missing"},
	} {
		var stdout, stderr bytes.Buffer
		require.Equal(t, 2, run(context.Background(), args, &stdout, &stderr), "%v", args)
		require.Empty(t, stdout.String())
	}
	data, err := os.ReadFile("../../testdata/eval/retrieval.v1.json")
	require.NoError(t, err)
	var fixture eval.RetrievalFixture
	require.NoError(t, json.Unmarshal(data, &fixture))
	fixture.Cases[10].ExpectedOutcomes["hybrid_rrf"] = "permission_denied"
	data, err = json.Marshal(fixture)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "bad-expectation.json")
	require.NoError(t, os.WriteFile(file, data, 0o600))
	var stdout, stderr bytes.Buffer
	require.Equal(t, 1, run(context.Background(), []string{"-suite", "retrieval", "-retrieval-fixture", file}, &stdout, &stderr))
	var report eval.RetrievalReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.False(t, report.GatePassed)
	require.False(t, report.Strategies[3].Cases[10].OutcomeMatched)
}
