package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestScriptedRealReactFaultsAndReplay(t *testing.T) {
	for _, c := range scriptCases() {
		t.Run(c.ID, func(t *testing.T) {
			got, err := runScriptCase(context.Background(), scriptSnapshot(), c)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Passed {
				t.Fatalf("fault behavior: %+v", got)
			}
			if got.ReplayChecked != (got.Observed.Status == "completed") {
				t.Fatal("replay denominator is wrong")
			}
			if got.Observed.Status == "completed" && !reflect.DeepEqual(got.SelectedIDs, []string{"mouse"}) {
				t.Fatal("not grounded")
			}
		})
	}
}

func TestScriptReportRepeatableAndNoRealModelClaims(t *testing.T) {
	a, err := RunScripted(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RunScripted(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || a.Requests != 16 || a.Passed != 16 || !a.GatePassed || a.ModelCalls != 33 {
		t.Fatalf("non-repeatable script results: %+v / %+v", a, b)
	}
	if a.ExternalModelStatus != "not_run" || a.Model != "scripted_fake" || a.TokenUsage != nil || len(a.SuiteSHA256) != 64 {
		t.Fatal("misleading provenance")
	}
	promptHash := ""
	for _, c := range a.Cases {
		if c.Observed.ModelCalls == 0 {
			continue
		}
		if len(c.PromptSHA256) != 64 {
			t.Fatal("prompt fingerprint missing")
		}
		if promptHash == "" {
			promptHash = c.PromptSHA256
		} else if promptHash != c.PromptSHA256 {
			t.Fatal("prompt differs across cases")
		}
	}
	md := ScriptedMarkdown(a)
	for _, want := range []string{"16/16", "not_run", "不是模型自主纠错", "不等于模型调用次数"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q", want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunScripted(ctx, "test"); !errors.Is(err, context.Canceled) {
		t.Fatal("outer cancellation swallowed")
	}
}

func TestScriptModelCountersSurviveBindingAndExhaustionFails(t *testing.T) {
	m := &scriptModel{steps: []scriptStep{{}}, state: &scriptModelState{}}
	clone, err := m.WithTools([]*schema.ToolInfo{{Name: "search_products"}, {Name: "select_bundle"}})
	if err != nil {
		t.Fatal(err)
	}
	in := []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("query")}
	if _, err := clone.Generate(context.Background(), in); err != nil || m.state.calls != 1 {
		t.Fatal("clone counters lost")
	}
	if _, err := clone.Generate(context.Background(), in); err == nil || !m.state.invalid || m.state.calls != 2 {
		t.Fatal("exhausted script faked final success")
	}
}

func TestScriptHarnessCannotHideFeedbackOrExecutionMismatch(t *testing.T) {
	c := scriptCases()[0]
	c.Steps[1].PreviousToolError = "invalid_argument" // 实际搜索成功；不允许凭脚本推进。
	out, err := runScriptCase(context.Background(), scriptSnapshot(), c)
	if err != nil || out.Passed || out.Checks["tool_feedback"] {
		t.Fatalf("feedback mismatch hidden: %+v %v", out, err)
	}
	c = scriptCases()[0]
	c.Expected.ModelCalls++
	out, err = runScriptCase(context.Background(), scriptSnapshot(), c)
	if err != nil || out.Passed || out.Checks["expected_execution"] {
		t.Fatalf("counter mismatch hidden: %+v %v", out, err)
	}
}

func TestArchivedScriptedReportMatchesVersionedFixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/eval/scripted.v1/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var archived ScriptReport
	if err := json.Unmarshal(data, &archived); err != nil {
		t.Fatal(err)
	}
	current, err := RunScripted(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	// 运行平台/代码标记可不同，脚本、输入、Prompt 和非计时行为必须与归档一致。
	if archived.SuiteSHA256 != current.SuiteSHA256 || archived.SnapshotSHA256 != current.SnapshotSHA256 ||
		archived.Requests != current.Requests || archived.Passed != current.Passed || archived.ModelCalls != current.ModelCalls ||
		archived.GatePassed != current.GatePassed || archived.ExternalModelStatus != "not_run" || archived.TokenUsage != nil || !reflect.DeepEqual(archived.Cases, current.Cases) {
		t.Fatal("script fixture/results changed without versioning")
	}
	md, err := os.ReadFile("../../testdata/eval/scripted.v1/report.md")
	if err != nil || string(md) != ScriptedMarkdown(archived) {
		t.Fatalf("script formats disagree: %v", err)
	}
}
