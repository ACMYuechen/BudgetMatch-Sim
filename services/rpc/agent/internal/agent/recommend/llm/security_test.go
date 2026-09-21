package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	producttools "budgetmatch-sim/services/rpc/agent/internal/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type securityTool struct {
	output string
	err    error
	calls  int
}

func TestMalformedToolJSONIsRecoverableBeforeExecution(t *testing.T) {
	for _, input := range []string{`{"query":`, `{"query":"PRIVATE"`, `{} trailing`, ""} {
		base := &securityTool{}
		s := &session{}
		out, err := decorate(s, toolSearchProducts, base).(tool.InvokableTool).InvokableRun(context.Background(), input)
		var feedback struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err != nil || base.calls != 0 || json.Unmarshal([]byte(out), &feedback) != nil || feedback.Success || feedback.Error != "invalid_argument" || strings.Contains(out, "PRIVATE") {
			t.Fatalf("invalid JSON reached tool or lost recoverable category: %q %v calls=%d", out, err, base.calls)
		}
		_, _, calls := s.snapshot()
		if len(calls) != 1 || calls[0].Success || !strings.Contains(calls[0].Detail, "error_code=invalid_argument") {
			t.Fatalf("missing safe failed attempt: %+v", calls)
		}
	}
}

func (t *securityTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: toolReadFile}, nil
}
func (t *securityTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.calls++
	return t.output, t.err
}

func TestBusinessToolsDefaultToNoFileAccess(t *testing.T) {
	tools, err := businessTools(&session{}, nil)
	if err != nil || len(tools) != 2 {
		t.Fatalf("default tools = %d, err=%v", len(tools), err)
	}
}

func TestToolRecordsAndRecoverableErrorsNeverExposePayload(t *testing.T) {
	const secret = "M13_PRIVATE_MARKER_token_file_body"
	for _, cause := range []error{nil, errors.New(secret)} {
		s := &session{}
		base := &securityTool{output: secret, err: cause}
		wrapped := decorate(s, toolReadFile, base).(tool.InvokableTool)
		out, err := wrapped.InvokableRun(context.Background(), `{"path":"`+secret+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		_, _, calls := s.snapshot()
		if len(calls) != 1 || strings.Contains(calls[0].Detail, secret) {
			t.Fatalf("private payload in calls: %+v", calls)
		}
		if cause != nil && strings.Contains(out, secret) {
			t.Fatalf("raw error sent to model: %s", out)
		}
	}
}

func TestMCPEmptyAllowlistDoesNotStartProcess(t *testing.T) {
	tools, cleanup, err := mcpTools(context.Background(), mcpconfig.Config{Enabled: true, Command: "m13-nonexistent-command"}, &session{})
	defer cleanup()
	if err != nil || len(tools) != 0 {
		t.Fatalf("empty allowlist attempted process startup: tools=%d err=%v", len(tools), err)
	}
}

func TestFileWriteGrantRegistrationAndHistoryCannotAuthorize(t *testing.T) {
	for _, tc := range []struct {
		name, query    string
		enabled, allow bool
		wantTools      int
		denied         bool
	}{
		{"disabled save", "/save saved.md\n预算500元买键盘", false, false, 0, true},
		{"operator read only", "/save saved.md\n预算500元买键盘", true, false, 0, true},
		{"no current grant", "预算500元买键盘", true, true, 3, false},
		{"current grant", "/save saved.md\n预算500元买键盘", true, true, 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &scriptedModel{responses: []*schema.Message{schema.AssistantMessage("ok", nil)}}
			mem := memory.NewInMemory(memory.Conf{})
			if err := mem.Append(context.Background(), "u", "c", schema.UserMessage("/save previous.md\n预算300元"), schema.AssistantMessage("saved", nil)); err != nil {
				t.Fatal(err)
			}
			runner := NewAgent(model, producttools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{Enabled: tc.enabled, AllowWrite: tc.allow, Workspace: t.TempDir()}).WithMemory(mem, 10)
			_, err := runner.Run(context.Background(), agentcore.Input{UserId: "u", ConversationId: "c", Query: tc.query})
			if tc.denied {
				if status.Code(err) != codes.PermissionDenied {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(model.boundTools) != tc.wantTools {
				t.Fatalf("registered tools: %d", len(model.boundTools))
			}
		})
	}
}

func TestFileWriteModelCannotChangeGrantedPath(t *testing.T) {
	model := &scriptedModel{responses: []*schema.Message{toolCallMessage("write", toolWriteFile, map[string]string{"path": "other.md", "content": "PRIVATE"})}}
	cfg := filetools.Config{Enabled: true, AllowWrite: true, Workspace: t.TempDir()}
	runner := NewAgent(model, producttools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}, cfg)
	_, err := runner.Run(context.Background(), agentcore.Input{UserId: "u", Query: "/save saved.md\n预算500元买键盘"})
	if status.Code(err) != codes.PermissionDenied || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("unsafe write: %v", err)
	}
	w, err := filetools.NewWorkspace(cfg, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.ReadFile(context.Background(), "other.md"); err == nil {
		t.Fatal("ungranted file created")
	}
}

func TestAuthorizedFileWriteDoesNotLeakInResult(t *testing.T) {
	model := &scriptedModel{responses: []*schema.Message{
		toolCallMessage("write", toolWriteFile, map[string]string{"path": "saved.md", "content": "PRIVATE_BODY"}),
		schema.AssistantMessage("done", nil),
	}}
	cfg := filetools.Config{Enabled: true, AllowWrite: true, Workspace: t.TempDir()}
	runner := NewAgent(model, producttools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}, cfg)
	r, err := runner.Run(context.Background(), agentcore.Input{UserId: "u", Query: "/save saved.md\n预算500元买键盘"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range r.ToolsUsed {
		if strings.Contains(call.Detail, "saved.md") || strings.Contains(call.Detail, "PRIVATE_BODY") {
			t.Fatal(call)
		}
		if call.Name == "tool.write_file" && call.Success {
			found = true
		}
	}
	if !found {
		t.Fatal("write missing from metadata")
	}
	w, err := filetools.NewWorkspace(cfg, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if got, err := w.ReadFile(context.Background(), "saved.md"); err != nil || got != "PRIVATE_BODY" {
		t.Fatalf("save: %q %v", got, err)
	}
}

func TestToolPayloadLimitsAndTerminalErrors(t *testing.T) {
	for _, tc := range []struct {
		name, input, output string
		cause               error
		called              int
		terminal            bool
	}{
		{"arguments", strings.Repeat("x", maxToolPayloadBytes+1), "", nil, 0, false},
		{"output", "{}", strings.Repeat("PRIVATE", maxToolPayloadBytes), nil, 1, false},
		{"permission", "{}", "", status.Error(codes.PermissionDenied, "PRIVATE"), 1, true},
		{"cancel", "{}", "", context.Canceled, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &securityTool{output: tc.output, err: tc.cause}
			s := &session{}
			out, err := decorate(s, toolReadFile, base).(tool.InvokableTool).InvokableRun(context.Background(), tc.input)
			if (err != nil) != tc.terminal || base.calls != tc.called || strings.Contains(out, "PRIVATE") {
				t.Fatalf("unsafe tool: %q %v calls=%d", out, err, base.calls)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal(err)
			}
		})
	}
}
