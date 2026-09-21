package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/eval"
)

func fixtureArgs() []string {
	return []string{"-snapshot", "../../testdata/eval/products.v1.json", "-cases", "../../testdata/eval/cases.v1.jsonl"}
}

func TestGenerateThenCheckPendingWithoutOverwriting(t *testing.T) {
	var out, stderr bytes.Buffer
	dir := filepath.Join(t.TempDir(), "review")
	args := append(fixtureArgs(), "-out", dir, "-revision", "test")
	if code := run(args, &out, &stderr); code != 0 {
		t.Fatalf("generation: %d %s", code, stderr.String())
	}
	path := filepath.Join(dir, "review.json")
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, out.Bytes()) {
		t.Fatal("stdout/template mismatch")
	}
	md, err := os.ReadFile(filepath.Join(dir, "review.md"))
	if err != nil || !bytes.Contains(md, []byte("人工复核工作单")) {
		t.Fatal("worksheet missing")
	}
	out.Reset()
	stderr.Reset()
	if code := run(args, &out, &stderr); code != 2 {
		t.Fatal("existing packet overwritten")
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(data, again) {
		t.Fatal("previous packet changed")
	}
	out.Reset()
	stderr.Reset()
	if code := run(append(fixtureArgs(), "-check", path), &out, &stderr); code != 1 {
		t.Fatalf("pending record exit=%d %s", code, stderr.String())
	}
	var summary eval.ReviewSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil || summary.Pending != 64 || summary.RecordsComplete {
		t.Fatal("pending status hidden")
	}
}

func TestCheckCompleteRecordsStillDoNotCertifyHumans(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := run(fixtureArgs(), &out, &stderr); code != 0 {
		t.Fatal(code)
	}
	var r eval.Review
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	// 临时测试声明，不归档为真实人工复核。
	r.Reviewer = eval.ReviewReviewer{ID: "test-reviewer", Human: true, Independent: true, SnapshotChecked: true}
	for i := range r.Cases {
		r.Cases[i].Decision, r.Cases[i].ReviewedAt = "accepted", "2026-09-17T18:00:00Z"
		r.Cases[i].Checks = eval.ReviewChecks{Constraints: true, Requirements: true, SKULabels: true, Feasibility: true, Outcome: true, SplitFamily: true}
	}
	data, _ := json.Marshal(r)
	path := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if code := run(append(fixtureArgs(), "-check", path), &out, &stderr); code != 0 {
		t.Fatalf("records: %d %s", code, stderr.String())
	}
	var summary eval.ReviewSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil || !summary.RecordsComplete || summary.HumanIdentityVerified || summary.AcceptanceStatus != "not_performed" {
		t.Fatal("checker certified human acceptance")
	}
	r.CasesSHA256 = strings.Repeat("0", 64)
	data, _ = json.Marshal(r)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if code := run(append(fixtureArgs(), "-check", path), &out, &stderr); code != 2 || out.Len() != 0 {
		t.Fatal("stale review emitted a valid status")
	}
}

func TestReviewCLIRejectsAmbiguousModesAndFormats(t *testing.T) {
	for _, args := range [][]string{
		{"-format", "html"}, {"-split", "dev"}, {"-check", "missing", "-out", "ignored"},
		{"-check", "missing", "-revision", "ignored"}, {"-snapshot", "/definitely/missing"}, {"unexpected"},
	} {
		var out, stderr bytes.Buffer
		if code := run(args, &out, &stderr); code != 2 || out.Len() != 0 {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	var out, stderr bytes.Buffer
	if code := run(append(fixtureArgs(), "-format", "markdown"), &out, &stderr); code != 0 || !strings.HasPrefix(out.String(), "# Agent 数据集人工复核工作单") {
		t.Fatal("Markdown generation failed")
	}
}
