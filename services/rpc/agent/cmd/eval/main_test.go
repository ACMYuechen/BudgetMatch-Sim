package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/eval"
)

func TestCLIReportsKnownGateFailureAndWritesMatchingFormats(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "report")
	args := []string{"-snapshot", "../../testdata/eval/products.v1.json", "-cases", "../../testdata/eval/cases.v1.jsonl", "-revision", "test", "-out", dir}
	var out, stderr bytes.Buffer
	code := run(context.Background(), args, &out, &stderr)
	if code != 0 && code != 1 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var r eval.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Summary.GatePassed != (code == 0) || r.Summary.Requests != 64 {
		t.Fatal("gate failures hidden")
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil || !bytes.Equal(data, out.Bytes()) {
		t.Fatalf("JSON artifact mismatch: %v", err)
	}
	md, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil || string(md) != eval.Markdown(r) {
		t.Fatalf("Markdown artifact mismatch: %v", err)
	}
	out.Reset()
	stderr.Reset()
	if code := run(context.Background(), args, &out, &stderr); code != 2 {
		t.Fatalf("existing reports overwritten: %d", code)
	}
	again, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if !bytes.Equal(again, data) {
		t.Fatal("existing report changed")
	}
}

func TestCLIInvalidArgsAndMarkdown(t *testing.T) {
	for _, args := range [][]string{{"-format", "invalid"}, {"-unknown"}, {"-snapshot", "/definitely/missing/eval.json"}} {
		var out, err bytes.Buffer
		if run(context.Background(), args, &out, &err) != 2 {
			t.Fatal("bad CLI input accepted")
		}
	}
	var out, err bytes.Buffer
	args := []string{"-snapshot", "../../testdata/eval/products.v1.json", "-cases", "../../testdata/eval/cases.v1.jsonl", "-format", "markdown"}
	if code := run(context.Background(), args, &out, &err); (code != 0 && code != 1) || !strings.HasPrefix(out.String(), "# Agent 离线规则基线") {
		t.Fatalf("markdown output: %d %s", code, err.String())
	}
}
