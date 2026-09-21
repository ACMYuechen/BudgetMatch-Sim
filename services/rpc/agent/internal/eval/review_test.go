package eval

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func reviewFixture(t *testing.T) (Dataset, Review) {
	t.Helper()
	d := fixture(t)
	r, err := NewReview(d, "test+worktree")
	if err != nil {
		t.Fatal(err)
	}
	return d, r
}

// 仅为校验器测试构造声明，不作为真实人工复核证据，不归档这些记录。
func acceptForTest(r *Review, index int) {
	r.Reviewer = ReviewReviewer{ID: "test-reviewer", Human: true, Independent: true, SnapshotChecked: true}
	e := &r.Cases[index]
	e.Decision, e.ReviewedAt = "accepted", "2026-09-17T18:00:00+08:00"
	e.Checks = ReviewChecks{true, true, true, true, true, true}
}

func TestReviewTemplateNeverClaimsHumanAcceptance(t *testing.T) {
	d, r := reviewFixture(t)
	before, _ := json.Marshal(d)
	s, err := CheckReview(d, r)
	if err != nil || s.Total != 64 || s.Pending != 64 || s.Accepted != 0 || s.RecordsComplete || s.ReviewerDeclared || s.HumanIdentityVerified || s.AcceptanceStatus != "not_performed" {
		t.Fatalf("misleading template status: %+v %v", s, err)
	}
	for _, e := range r.Cases {
		if e.Decision != "pending" || e.Checks != (ReviewChecks{}) || e.Notes != "" || e.ReviewedAt != "" || len(e.CaseSHA256) != 64 {
			t.Fatal("template invented review activity")
		}
	}
	again, err := NewReview(d, "test+worktree")
	if err != nil || !reflect.DeepEqual(r, again) {
		t.Fatal("template is not deterministic")
	}
	after, _ := json.Marshal(d)
	if string(before) != string(after) {
		t.Fatal("source dataset was changed")
	}
	if _, err := NewReview(miniDataset(), "test"); err == nil {
		t.Fatal("missing source hashes accepted")
	}
}

func TestReviewProgressAndRecordedCompletionRemainSeparateFromAcceptance(t *testing.T) {
	d, r := reviewFixture(t)
	acceptForTest(&r, 0)
	r.Cases[1].Decision, r.Cases[1].ReviewedAt, r.Cases[1].Notes = "changes_requested", "2026-09-17T18:00:00Z", "Need to recheck quiet-property SKU labels."
	s, err := CheckReview(d, r)
	if err != nil || s.Accepted != 1 || s.ChangesRequested != 1 || s.Pending != 62 || s.RecordsComplete {
		t.Fatalf("partial progress: %+v %v", s, err)
	}
	for i := range r.Cases {
		acceptForTest(&r, i)
	}
	s, err = CheckReview(d, r)
	if err != nil || !s.RecordsComplete || s.Accepted != 64 || s.HumanIdentityVerified || s.AcceptanceStatus != "not_performed" || d.Snapshot.AnnotationStatus != "pending_human_review" {
		t.Fatalf("record completion was misinterpreted as acceptance: %+v %v", s, err)
	}
	for _, field := range []string{"human", "independent", "snapshot"} {
		copy := r
		switch field {
		case "human":
			copy.Reviewer.Human = false
		case "independent":
			copy.Reviewer.Independent = false
		case "snapshot":
			copy.Reviewer.SnapshotChecked = false
		}
		s, err = CheckReview(d, copy)
		if err != nil || s.RecordsComplete {
			t.Fatalf("missing %s declaration passed: %+v %v", field, s, err)
		}
	}
	// 允许按复核顺序排列记录，但不能漏项或重复。
	r.Cases[0], r.Cases[1] = r.Cases[1], r.Cases[0]
	if s, err := CheckReview(d, r); err != nil || !s.RecordsComplete {
		t.Fatalf("reordering rejected: %v", err)
	}
}

func TestReviewRejectsStaleIncompleteAndMalformedRecords(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Dataset, *Review)
	}{
		{"schema", func(_ *Dataset, r *Review) { r.SchemaVersion++ }},
		{"snapshot version", func(_ *Dataset, r *Review) { r.SnapshotVersion = "other" }},
		{"snapshot hash", func(_ *Dataset, r *Review) { r.SnapshotSHA256 = strings.Repeat("0", 64) }},
		{"cases hash", func(_ *Dataset, r *Review) { r.CasesSHA256 = strings.Repeat("0", 64) }},
		{"case hash", func(_ *Dataset, r *Review) { r.Cases[0].CaseSHA256 = strings.Repeat("0", 64) }},
		{"changed input", func(d *Dataset, _ *Review) { d.Cases[0].Input.Query += " please" }},
		{"missing", func(_ *Dataset, r *Review) { r.Cases = r.Cases[1:] }},
		{"duplicate", func(_ *Dataset, r *Review) { r.Cases[0] = r.Cases[1] }},
		{"unknown", func(_ *Dataset, r *Review) { r.Cases[0].ID = "unknown" }},
		{"unknown decision", func(_ *Dataset, r *Review) { r.Cases[0].Decision = "auto_approved" }},
		{"unattributed decision", func(_ *Dataset, r *Review) { acceptForTest(r, 0); r.Reviewer.ID = "" }},
		{"whitespace reviewer", func(_ *Dataset, r *Review) { r.Reviewer.ID = " reviewer " }},
		{"long reviewer", func(_ *Dataset, r *Review) { r.Reviewer.ID = strings.Repeat("人", 129) }},
		{"missing checks", func(_ *Dataset, r *Review) { acceptForTest(r, 0); r.Cases[0].Checks.Requirements = false }},
		{"bad timestamp", func(_ *Dataset, r *Review) { acceptForTest(r, 0); r.Cases[0].ReviewedAt = "yesterday" }},
		{"missing timestamp", func(_ *Dataset, r *Review) { acceptForTest(r, 0); r.Cases[0].ReviewedAt = "" }},
		{"pending timestamp", func(_ *Dataset, r *Review) { r.Cases[0].ReviewedAt = "2026-09-17T18:00:00Z" }},
		{"changes without reason", func(_ *Dataset, r *Review) {
			acceptForTest(r, 0)
			r.Cases[0].Decision = "changes_requested"
			r.Cases[0].Notes = " "
		}},
		{"notes too long", func(_ *Dataset, r *Review) { r.Cases[0].Notes = strings.Repeat("字", 2049) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, r := reviewFixture(t)
			tc.change(&d, &r)
			if _, err := CheckReview(d, r); err == nil {
				t.Fatal("invalid review accepted")
			}
		})
	}
	for _, data := range []string{`{"unknown":true}`, `{} {}`, `{`, strings.Repeat("x", maxDatasetBytes+1)} {
		if _, err := LoadReview(strings.NewReader(data)); err == nil {
			t.Fatal("malformed review JSON accepted")
		}
	}
}

func TestReviewPacketContainsAllInputsWithoutPolicyResults(t *testing.T) {
	d, r := reviewFixture(t)
	md := ReviewMarkdown(d, r)
	if strings.Count(md, "### ") != 64 {
		t.Fatal("missing review cases")
	}
	for _, c := range d.Cases {
		if !strings.Contains(md, reviewJSON(c)) {
			t.Fatalf("lost input/history/oracle: %s", c.ID)
		}
	}
	for _, p := range d.Snapshot.Products {
		if !strings.Contains(md, "| "+p.ID+" |") {
			t.Fatal("snapshot product omitted")
		}
	}
	for _, unwanted := range []string{"selected_ids", "task_success", "latency_p95_ms", "gate_passed"} {
		if strings.Contains(md, unwanted) {
			t.Fatalf("policy output leaked into label review: %s", unwanted)
		}
	}
	d.Snapshot.Products[0].Name = "[click](https://example.invalid) <script>|\ntext"
	md = ReviewMarkdown(d, r)
	if strings.Contains(md, "<script>") || strings.Contains(md, "[click](") || strings.Contains(md, "|\ntext") {
		t.Fatal("snapshot content escaped Markdown data boundary")
	}
	s, _ := CheckReview(fixture(t), r)
	if !strings.Contains(ReviewSummaryMarkdown(s), "不认证声明真实性") {
		t.Fatal("missing verification limitation")
	}
}

func TestArchivedReviewTemplateMatchesFrozenInputs(t *testing.T) {
	d := fixture(t)
	file, err := os.Open("../../testdata/eval/review.v1/review.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r, err := LoadReview(file)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := NewReview(d, r.CodeRevision)
	if err != nil || !reflect.DeepEqual(expected, r) {
		t.Fatal("pending review template drifted; keep human records in a separate file")
	}
	md, err := os.ReadFile("../../testdata/eval/review.v1/review.md")
	if err != nil || string(md) != ReviewMarkdown(d, r) {
		t.Fatalf("review packet formats disagree: %v", err)
	}
}
