package eval

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

type CaseChange struct {
	ID                string `json:"id"`
	BeforeStatus      string `json:"before_status"`
	AfterStatus       string `json:"after_status"`
	BeforeTaskSuccess bool   `json:"before_task_success"`
	AfterTaskSuccess  bool   `json:"after_task_success"`
}

// Comparison 保留两侧的分子/分母，不把不同数据集的百分比相减冒充提升。
type Comparison struct {
	BeforeRevision   string       `json:"before_revision"`
	AfterRevision    string       `json:"after_revision"`
	Before           Summary      `json:"before"`
	After            Summary      `json:"after"`
	OutcomeFixed     []string     `json:"outcome_fixed"`
	OutcomeRegressed []string     `json:"outcome_regressed"`
	TaskFixed        []string     `json:"task_fixed"`
	TaskRegressed    []string     `json:"task_regressed"`
	Changes          []CaseChange `json:"changes"`
}

func LoadReport(reader io.Reader) (Report, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return Report{}, errors.New("cannot read report or report exceeds 16 MiB")
	}
	var r Report
	if err := decodeStrict(data, &r); err != nil {
		return Report{}, errors.New("invalid baseline report JSON")
	}
	return r, nil
}

func Compare(before, after Report) (*Comparison, error) {
	a, b := before.Metadata, after.Metadata
	if before.SchemaVersion != 1 || after.SchemaVersion != 1 ||
		len(a.SnapshotSHA256) != 64 || len(a.CasesSHA256) != 64 ||
		a.SnapshotSHA256 != b.SnapshotSHA256 || a.CasesSHA256 != b.CasesSHA256 ||
		a.SnapshotVersion != b.SnapshotVersion || a.AnnotationStatus != b.AnnotationStatus ||
		a.Split != b.Split || a.TopK != b.TopK || a.CachePolicy != b.CachePolicy || a.Concurrency != b.Concurrency ||
		a.Model != "not_used" || b.Model != "not_used" || a.Prompt != b.Prompt || a.Embedding != b.Embedding {
		return nil, errors.New("reports must use the same rule-evaluation data, split, top-k and measurement protocol")
	}
	for _, r := range []Report{before, after} {
		if len(r.Cases) == 0 || !reflect.DeepEqual(Summarize(r.Cases), r.Summary) {
			return nil, errors.New("report summary does not match case results")
		}
	}
	old := map[string]CaseResult{}
	for _, c := range before.Cases {
		if _, exists := old[c.ID]; exists || c.ID == "" {
			return nil, errors.New("duplicate or empty baseline case ID")
		}
		old[c.ID] = c
	}
	c := &Comparison{BeforeRevision: a.CodeRevision, AfterRevision: b.CodeRevision, Before: before.Summary, After: after.Summary,
		OutcomeFixed: []string{}, OutcomeRegressed: []string{}, TaskFixed: []string{}, TaskRegressed: []string{}, Changes: []CaseChange{}}
	for _, next := range after.Cases {
		prev, exists := old[next.ID]
		if !exists || prev.Split != next.Split || prev.Scenario != next.Scenario || prev.ExpectedStatus != next.ExpectedStatus ||
			prev.Feasibility != next.Feasibility || prev.RequirementsTotal != next.RequirementsTotal || prev.RelevantTotal != next.RelevantTotal {
			return nil, errors.New("case sets or annotations differ")
		}
		delete(old, next.ID)
		if !prev.OutcomeMatched && next.OutcomeMatched {
			c.OutcomeFixed = append(c.OutcomeFixed, next.ID)
		}
		if prev.OutcomeMatched && !next.OutcomeMatched {
			c.OutcomeRegressed = append(c.OutcomeRegressed, next.ID)
		}
		if !prev.TaskSuccess && next.TaskSuccess {
			c.TaskFixed = append(c.TaskFixed, next.ID)
		}
		if prev.TaskSuccess && !next.TaskSuccess {
			c.TaskRegressed = append(c.TaskRegressed, next.ID)
		}
		// 延迟单列在汇总中，不把计时噪声当成行为变化。
		left, right := prev, next
		left.Timing, right.Timing = Timing{}, Timing{}
		if !reflect.DeepEqual(left, right) {
			c.Changes = append(c.Changes, CaseChange{next.ID, prev.Status, next.Status, prev.TaskSuccess, next.TaskSuccess})
		}
	}
	if len(old) != 0 {
		return nil, errors.New("current report omits baseline cases")
	}
	return c, nil
}

func comparisonMarkdown(c *Comparison) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n## 同数据版本对比\n\n%s → %s（调用者提供的代码标记）。\n\n", markdownText(c.BeforeRevision), markdownText(c.AfterRevision))
	fmt.Fprintln(&b, "| 指标 | 之前 | 当前 |\n| --- | --- | --- |")
	for _, m := range []struct {
		name          string
		before, after Ratio
	}{
		{"预期终态", c.Before.OutcomeAccuracy, c.After.OutcomeAccuracy},
		{"可满足任务", c.Before.SatisfiableSuccess, c.After.SatisfiableSuccess},
		{"需求覆盖", c.Before.RequirementCoverage, c.After.RequirementCoverage},
		{"Recall@K", c.Before.RecallAtK, c.After.RecallAtK},
		{"硬约束违规", c.Before.HardViolationRate, c.After.HardViolationRate},
	} {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", m.name, formatRatio(m.before), formatRatio(m.after))
	}
	list := func(ids []string) string {
		if len(ids) == 0 {
			return "无"
		}
		return markdownText(strings.Join(ids, ", "))
	}
	fmt.Fprintf(&b, "\n终态修复：%s；终态退化：%s。\n任务修复：%s；任务退化：%s。\n", list(c.OutcomeFixed), list(c.OutcomeRegressed), list(c.TaskFixed), list(c.TaskRegressed))
	fmt.Fprint(&b, "\n保留原始标注和划分；修复已知 holdout 反例后，该集合属于已知回归集，不再声称是盲测。环境负载未控制，延迟差异不作为性能提升证据。\n")
	return b.String()
}
