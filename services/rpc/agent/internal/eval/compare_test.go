package eval

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func archivedRule(t *testing.T, path string) Report {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := LoadReport(f)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestComparePreservesOriginalCounterexamples(t *testing.T) {
	before := archivedRule(t, "../../testdata/eval/baseline.v1/report.json")
	after, err := Run(context.Background(), fixture(t), Options{Revision: "test+worktree"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.OutcomeFixed, []string{"history_budget_down", "history_unsatisfiable_after_cut"}) ||
		!reflect.DeepEqual(c.TaskFixed, []string{"history_budget_down"}) || len(c.OutcomeRegressed) != 0 || len(c.TaskRegressed) != 0 || len(c.Changes) != 2 {
		t.Fatalf("unexpected changes: %+v", c)
	}
	if c.Before.GatePassed || !c.After.GatePassed || c.Before.SatisfiableSuccess.Numerator != 24 || c.After.SatisfiableSuccess.Numerator != 25 || c.After.SatisfiableSuccess.Denominator != 40 {
		t.Fatalf("wrong paired summary: %+v", c)
	}
	reverse, err := Compare(after, before)
	if err != nil || !reflect.DeepEqual(reverse.OutcomeRegressed, c.OutcomeFixed) || !reflect.DeepEqual(reverse.TaskRegressed, c.TaskFixed) {
		t.Fatalf("regressions hidden: %+v %v", reverse, err)
	}
	after.Comparison = c
	if !strings.Contains(Markdown(after), "不再声称是盲测") {
		t.Fatal("known holdout caveat missing")
	}
}

func TestCompareRejectsIncompatibleOrTamperedReports(t *testing.T) {
	before := archivedRule(t, "../../testdata/eval/baseline.v1/report.json")
	for _, tc := range []struct {
		name   string
		change func(*Report)
	}{
		{"hash", func(r *Report) { r.Metadata.CasesSHA256 = strings.Repeat("0", 64) }},
		{"snapshot", func(r *Report) { r.Metadata.SnapshotSHA256 = "" }},
		{"split", func(r *Report) { r.Metadata.Split = "dev" }},
		{"top-k", func(r *Report) { r.Metadata.TopK = 1 }},
		{"model", func(r *Report) { r.Metadata.Model = "scripted_fake" }},
		{"schema", func(r *Report) { r.SchemaVersion = 99 }},
		{"summary", func(r *Report) { r.Summary.GatePassed = true }},
		{"duplicate", func(r *Report) { r.Cases[1].ID = r.Cases[0].ID }},
		{"annotation", func(r *Report) { r.Cases[0].ExpectedStatus = "error" }},
		{"missing", func(r *Report) { r.Cases = r.Cases[1:]; r.Summary = Summarize(r.Cases) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(before)
			var after Report
			if err := json.Unmarshal(data, &after); err != nil {
				t.Fatal(err)
			}
			tc.change(&after)
			if _, err := Compare(before, after); err == nil {
				t.Fatal("incompatible comparison accepted")
			}
		})
	}
	for _, invalid := range []string{`{"unknown":1}`, `{`, `{} {}`, strings.Repeat("x", (16<<20)+1)} {
		if _, err := LoadReport(strings.NewReader(invalid)); err == nil {
			t.Fatal("invalid baseline accepted")
		}
	}
}

func TestArchivedM22ComparisonMatchesBothReports(t *testing.T) {
	d := fixture(t)
	before := archivedRule(t, "../../testdata/eval/baseline.v1/report.json")
	after := archivedRule(t, "../../testdata/eval/baseline.v2/report.json")
	if after.Metadata.SnapshotSHA256 != d.SnapshotSHA256 || after.Metadata.CasesSHA256 != d.CasesSHA256 {
		t.Fatal("archived dataset changed")
	}
	comparison, err := Compare(before, after)
	if err != nil || !reflect.DeepEqual(comparison, after.Comparison) {
		t.Fatalf("comparison artifacts disagree: %v", err)
	}
	md, err := os.ReadFile("../../testdata/eval/baseline.v2/report.md")
	if err != nil || string(md) != Markdown(after) {
		t.Fatalf("rule formats disagree: %v", err)
	}
}
