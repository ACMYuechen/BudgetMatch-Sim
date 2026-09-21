package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/llm"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const scriptPrivatePayload = "SCRIPT_PRIVATE_PAYLOAD"

// 脚本仅控制模型 I/O，不代替 ReAct、工具、Service、结果校验或会话存储。
// 它证明预设故障下的代码行为，不证明模型能自主纠错或抵抗提示注入。
type scriptStep struct {
	Tool              string `json:"tool,omitempty"`
	Arguments         string `json:"arguments,omitempty"`
	PreviousToolError string `json:"previous_tool_error,omitempty"`
	Error             string `json:"error,omitempty"`
	Cancel            bool   `json:"cancel,omitempty"`
}

type scriptExpectation struct {
	Status           string `json:"status"`
	ErrorCode        string `json:"error_code,omitempty"`
	ModelCalls       int    `json:"model_calls"`
	ToolCalls        int    `json:"tool_calls"`
	ProviderCalls    int    `json:"provider_calls"`
	FallbackCalls    int    `json:"fallback_calls"`
	SelectorFallback bool   `json:"selector_fallback"`
}

type scriptCase struct {
	ID            string            `json:"id"`
	Steps         []scriptStep      `json:"steps"`
	ProviderFault string            `json:"provider_fault,omitempty"`
	Query         string            `json:"query"`
	MaxSteps      int               `json:"max_steps"`
	Expected      scriptExpectation `json:"expected"`
}

type ScriptCaseResult struct {
	ID                       string            `json:"id"`
	Expected                 scriptExpectation `json:"expected"`
	Observed                 scriptExpectation `json:"observed"`
	Checks                   map[string]bool   `json:"checks"`
	Violations               []string          `json:"violations"`
	SelectedIDs              []string          `json:"selected_ids"`
	PromptSHA256             string            `json:"prompt_sha256,omitempty"`
	ReplayExtraModelCalls    int               `json:"replay_extra_model_calls"`
	ReplayExtraProviderCalls int               `json:"replay_extra_provider_calls"`
	ReplayExtraFallbackCalls int               `json:"replay_extra_fallback_calls"`
	ReplayExtraTurns         int64             `json:"replay_extra_turns"`
	ReplayChecked            bool              `json:"replay_checked"`
	Passed                   bool              `json:"passed"`
}

type ScriptReport struct {
	SchemaVersion       int                `json:"schema_version"`
	SuiteVersion        string             `json:"suite_version"`
	SuiteSHA256         string             `json:"suite_sha256"`
	SnapshotSHA256      string             `json:"snapshot_sha256"`
	CodeRevision        string             `json:"code_revision"`
	GoVersion           string             `json:"go_version"`
	OS                  string             `json:"os"`
	Arch                string             `json:"arch"`
	Model               string             `json:"model"`
	ExternalModelStatus string             `json:"external_model_status"`
	TokenUsage          *int64             `json:"token_usage"`
	Requests            int                `json:"requests"`
	Passed              int                `json:"passed"`
	ModelCalls          int                `json:"model_calls"`
	GatePassed          bool               `json:"gate_passed"`
	Cases               []ScriptCaseResult `json:"cases"`
}

func scriptCases() []scriptCase {
	search := scriptStep{Tool: "search_products", Arguments: `{"query":"鼠标","keywords":["鼠标"],"budget_cents":0,"max_items":0}`}
	selectBundle := scriptStep{Tool: "select_bundle", Arguments: `{}`}
	final := scriptStep{}
	repairSearch := func(code string) scriptStep { step := search; step.PreviousToolError = code; return step }
	repairSelect := selectBundle
	repairSelect.PreviousToolError = "invalid_argument"
	success := func(id string, steps []scriptStep, models, calls, providers int) scriptCase {
		return scriptCase{ID: id, Steps: steps, Query: "预算50元，只买一件鼠标", MaxSteps: 12,
			Expected: scriptExpectation{Status: "completed", ModelCalls: models, ToolCalls: calls, ProviderCalls: providers}}
	}
	cases := []scriptCase{
		success("normal_tool_flow", []scriptStep{search, selectBundle, final}, 3, 2, 1),
		success("no_selection", []scriptStep{final}, 1, 0, 1),
		success("malformed_json_repaired", []scriptStep{{Tool: "search_products", Arguments: `{"query":`}, repairSearch("invalid_argument"), selectBundle, final}, 4, 3, 1),
		success("negative_limit_repaired", []scriptStep{{Tool: "search_products", Arguments: `{"query":"鼠标","budget_cents":-1}`}, repairSearch("invalid_argument"), selectBundle, final}, 4, 3, 1),
		success("unknown_sku_repaired", []scriptStep{search, {Tool: "select_bundle", Arguments: `{"candidate_ids":["invented"]}`}, repairSelect, final}, 4, 3, 1),
		success("inflated_limits_clamped", []scriptStep{
			{Tool: "search_products", Arguments: `{"query":"鼠标","keywords":["鼠标"],"budget_cents":1000000,"max_items":10}`},
			{Tool: "select_bundle", Arguments: `{"budget_cents":1000000,"max_items":10}`}, final}, 3, 2, 1),
		success("model_unavailable", []scriptStep{{Error: "unavailable"}}, 1, 0, 1),
		success("model_error_after_selection", []scriptStep{search, selectBundle, {Error: "unavailable"}}, 3, 2, 2),
		success("tool_transient_repaired", []scriptStep{search, repairSearch("rpc_unavailable"), selectBundle, final}, 4, 3, 2),
		success("unknown_tool", []scriptStep{{Tool: "unregistered_tool", Arguments: `{}`}}, 1, 1, 1),
		success("max_steps", []scriptStep{search, search, search}, 1, 1, 2),
		success("model_permission", []scriptStep{{Error: "permission_denied"}}, 1, 0, 0),
		success("tool_permission", []scriptStep{search}, 1, 1, 1),
		success("model_deadline", []scriptStep{{Error: "deadline_exceeded"}}, 1, 0, 0),
		success("cancel_during_model", []scriptStep{{Cancel: true}}, 1, 0, 0),
		success("invalid_text_before_model", nil, 0, 0, 0),
	}
	for i := range cases {
		c := &cases[i]
		switch c.ID {
		case "no_selection":
			c.Expected.SelectorFallback = true
		case "model_unavailable", "model_error_after_selection", "unknown_tool", "max_steps":
			c.Expected.FallbackCalls = 1
		case "tool_transient_repaired":
			c.ProviderFault = "once_unavailable"
		case "model_permission", "tool_permission":
			c.Expected.Status, c.Expected.ErrorCode = "error", "permission_denied"
		case "model_deadline":
			c.Expected.Status, c.Expected.ErrorCode = "error", "deadline_exceeded"
		case "cancel_during_model":
			c.Expected.Status, c.Expected.ErrorCode = "error", "canceled"
		case "invalid_text_before_model":
			c.Query = "预算改成0元"
			c.Expected.Status, c.Expected.ErrorCode = "rejected", "budget_text"
		}
		if c.ID == "max_steps" {
			c.MaxSteps = 2
		}
		if c.ID == "tool_permission" {
			c.ProviderFault = "permission_denied"
		}
	}
	return cases
}

func scriptSnapshot() Snapshot {
	return Snapshot{Version: "scripted-products-v1", Provenance: "synthetic", AnnotationStatus: "pending_human_review", Products: []Product{
		{ID: "mouse", Name: "入门鼠标", Category: "鼠标", PriceCents: 4900, Stock: 2, Sold: 1, Active: true},
		{ID: "expensive_mouse", Name: "高价鼠标", Category: "鼠标", PriceCents: 10000, Stock: 2, Sold: 100, Active: true},
	}}
}

func RunScripted(ctx context.Context, revision string) (ScriptReport, error) {
	if revision == "" {
		revision = "unknown"
	}
	cases, snapshot := scriptCases(), scriptSnapshot()
	scriptsJSON, _ := json.Marshal(cases)
	snapshotJSON, _ := json.Marshal(snapshot)
	r := ScriptReport{SchemaVersion: 1, SuiteVersion: "scripted-react-v1", SuiteSHA256: fmt.Sprintf("%x", sha256.Sum256(scriptsJSON)),
		SnapshotSHA256: fmt.Sprintf("%x", sha256.Sum256(snapshotJSON)), CodeRevision: revision, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		Model: "scripted_fake", ExternalModelStatus: "not_run", Requests: len(cases)}
	for _, c := range cases {
		if err := ctx.Err(); err != nil {
			return ScriptReport{}, err
		}
		out, err := runScriptCase(ctx, snapshot, c)
		if err != nil {
			return ScriptReport{}, fmt.Errorf("script case %s: %w", c.ID, err)
		}
		r.Cases = append(r.Cases, out)
		r.ModelCalls += out.Observed.ModelCalls
		if out.Passed {
			r.Passed++
		}
	}
	r.GatePassed = r.Passed == r.Requests && r.Requests > 0
	return r, nil
}

type scriptModelState struct {
	mu           sync.Mutex
	calls, tools int
	bound        []string
	invalid      bool
	promptHash   string
}

type scriptModel struct {
	steps  []scriptStep
	state  *scriptModelState // WithTools 克隆共享统计；不能只读原模型的游标。
	cancel context.CancelFunc
}

var _ model.ToolCallingChatModel = (*scriptModel)(nil)

func (m *scriptModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	for _, info := range infos {
		m.state.bound = append(m.state.bound, info.Name)
	}
	clone := *m
	return &clone, nil
}

func (m *scriptModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	i := m.state.calls
	m.state.calls++
	if i >= len(m.steps) {
		m.state.invalid = true
		return nil, errors.New("script exhausted")
	}
	step := m.steps[i]
	if i == 0 {
		if len(input) < 2 || input[0].Role != schema.System {
			m.state.invalid = true
		} else {
			m.state.promptHash = fmt.Sprintf("%x", sha256.Sum256([]byte(input[0].Content)))
		}
	} else if m.steps[i-1].Tool != "" {
		// 下一步必须实际收到上一工具的成功/失败反馈，不能凭脚本顺序冒充恢复。
		valid := false
		for _, msg := range input {
			if msg.Role != schema.Tool || msg.ToolCallID != fmt.Sprintf("script-%d", i-1) {
				continue
			}
			var feedback struct {
				Success *bool  `json:"success"`
				Error   string `json:"error"`
			}
			valid = json.Unmarshal([]byte(msg.Content), &feedback) == nil && feedback.Error == step.PreviousToolError
			if step.PreviousToolError != "" {
				valid = valid && feedback.Success != nil && !*feedback.Success
			}
			if strings.Contains(msg.Content, scriptPrivatePayload) {
				valid = false
			}
		}
		if !valid {
			m.state.invalid = true
			return nil, errors.New("script tool feedback mismatch")
		}
	}
	if step.Cancel {
		m.cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch step.Error {
	case "unavailable":
		return nil, status.Error(codes.Unavailable, scriptPrivatePayload)
	case "permission_denied":
		return nil, status.Error(codes.PermissionDenied, scriptPrivatePayload)
	case "deadline_exceeded":
		return nil, context.DeadlineExceeded
	}
	if step.Tool != "" {
		m.state.tools++
		return schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprintf("script-%d", i), Type: "function", Function: schema.FunctionCall{Name: step.Tool, Arguments: step.Arguments}}}), nil
	}
	return schema.AssistantMessage(scriptPrivatePayload+": invented product costs 1 cent", nil), nil
}

func (m *scriptModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type scriptProvider struct {
	inner    *snapshotProvider
	fault    string
	calls    int
	limitsOK bool
}

func (p *scriptProvider) Name() string { return "eval.script_snapshot" }
func (p *scriptProvider) SearchProducts(ctx context.Context, req tools.SearchProductsReq) ([]agent.ProductCandidate, error) {
	p.calls++
	p.limitsOK = p.limitsOK && req.BudgetCents == 5000 && req.MaxItems == 1
	if p.fault == "permission_denied" {
		return nil, status.Error(codes.PermissionDenied, scriptPrivatePayload)
	}
	if p.fault == "once_unavailable" && p.calls == 1 {
		return nil, status.Error(codes.Unavailable, scriptPrivatePayload)
	}
	return p.inner.SearchProducts(ctx, req)
}

type countedRule struct {
	agent.Agent
	calls int
}

func (a *countedRule) Run(ctx context.Context, in agent.Input) (*agent.Result, error) {
	a.calls++
	return a.Agent.Run(ctx, in)
}

func runScriptCase(parent context.Context, snapshot Snapshot, c scriptCase) (ScriptCaseResult, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	mem := memory.NewInMemory(memory.Conf{})
	p := &scriptProvider{inner: &snapshotProvider{products: snapshot.Products, topK: 10}, fault: c.ProviderFault, limitsOK: true}
	m := &scriptModel{steps: c.Steps, state: &scriptModelState{}, cancel: cancel}
	fallback := &countedRule{Agent: recommendagent.NewAgent(p, selector.NewBundleSelector()).WithMemory(mem, 20)}
	primary := llm.NewAgent(m, p, selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{}).WithMaxStep(c.MaxSteps).WithMemory(mem, 20)
	svc := recommendagent.NewService(fallback, primary, mem)
	in := agent.Input{UserId: "script-user", ConversationId: c.ID, TurnId: "turn", Query: c.Query}
	result, runErr := svc.Recommend(ctx, in)
	if err := parent.Err(); err != nil {
		return ScriptCaseResult{}, err
	}
	expected := Case{ID: c.ID, Expected: Expected{Status: c.Expected.Status, ErrorCode: c.Expected.ErrorCode, BudgetCents: 5000, MaxItems: 1,
		Feasibility: "satisfiable", AcceptableSKUs: []string{"mouse"}, Requirements: []Requirement{{ID: "mouse", AnyOfSKUs: []string{"mouse"}}}, Fallback: c.Expected.FallbackCalls > 0}}
	assessed := Assess(snapshot, expected, result, runErr, p.inner.ids)
	observed := scriptExpectation{Status: assessed.Status, ErrorCode: assessed.ErrorCode, ProviderCalls: p.calls, FallbackCalls: fallback.calls}
	if result != nil {
		for _, call := range result.ToolsUsed {
			if call.Name == "selector.fallback" {
				observed.SelectorFallback = true
			}
		}
	}
	m.state.mu.Lock()
	observed.ModelCalls, observed.ToolCalls = m.state.calls, m.state.tools
	bound := append([]string{}, m.state.bound...)
	invalid, promptHash := m.state.invalid, m.state.promptHash
	m.state.mu.Unlock()
	sort.Strings(bound)
	boundOK := reflect.DeepEqual(bound, []string{"search_products", "select_bundle"})
	if c.Expected.ModelCalls == 0 {
		boundOK = len(bound) == 0
	}
	after, _, err := mem.GetConversation(parent, in.UserId, in.ConversationId)
	if err != nil {
		return ScriptCaseResult{}, err
	}
	data, _ := json.Marshal(result)
	publicSafe := !strings.Contains(string(data), scriptPrivatePayload) && (runErr == nil || !strings.Contains(runErr.Error(), scriptPrivatePayload))
	wantTurns := int64(0)
	if runErr == nil {
		wantTurns = 1
	}
	out := ScriptCaseResult{ID: c.ID, Expected: c.Expected, Observed: observed, Violations: assessed.Violations, SelectedIDs: assessed.SelectedIDs, PromptSHA256: promptHash,
		Checks: map[string]bool{"expected_execution": observed == c.Expected, "tool_feedback": !invalid, "only_business_tools": boundOK,
			"provider_limits": p.limitsOK, "hard_constraints": len(assessed.Violations) == 0, "outcome": assessed.OutcomeMatched,
			"grounded_result":     runErr != nil || assessed.TaskSuccess && result.Summary == agent.BundleSummary(len(result.Items), result.TotalPriceCents, 5000),
			"public_payload_safe": publicSafe, "persistence": after.TurnCount == wantTurns}}
	if runErr == nil {
		turn, found, err := mem.FindTurn(parent, in.UserId, in.ConversationId, in.TurnId)
		if err != nil {
			return ScriptCaseResult{}, err
		}
		out.Checks["persistence"] = out.Checks["persistence"] && found && turn.Query == in.Query && turn.BudgetCents == 0 && turn.MaxItems == 0 && !strings.Contains(string(turn.ResultJSON), scriptPrivatePayload)
		out.ReplayChecked = true
		replayed, replayErr := svc.Recommend(parent, in)
		replayedJSON, _ := json.Marshal(replayed)
		afterReplay, _, err := mem.GetConversation(parent, in.UserId, in.ConversationId)
		if err != nil {
			return ScriptCaseResult{}, err
		}
		m.state.mu.Lock()
		out.ReplayExtraModelCalls = m.state.calls - observed.ModelCalls
		m.state.mu.Unlock()
		out.ReplayExtraProviderCalls, out.ReplayExtraFallbackCalls = p.calls-observed.ProviderCalls, fallback.calls-observed.FallbackCalls
		out.ReplayExtraTurns = afterReplay.TurnCount - after.TurnCount
		out.Checks["replay"] = replayErr == nil && string(data) == string(replayedJSON) && out.ReplayExtraModelCalls == 0 && out.ReplayExtraProviderCalls == 0 && out.ReplayExtraFallbackCalls == 0 && out.ReplayExtraTurns == 0
	}
	out.Passed = true
	for _, ok := range out.Checks {
		out.Passed = out.Passed && ok
	}
	return out, nil
}

func ScriptedMarkdown(r ScriptReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Agent 脚本模型容错评测\n\n版本：%s；代码：%s（调用者提供）；环境：%s / %s / %s。\n\n", markdownText(r.SuiteVersion), markdownText(r.CodeRevision), r.OS, r.Arch, r.GoVersion)
	fmt.Fprintf(&b, "脚本 SHA-256：`%s`；快照 SHA-256：`%s`。\n\n", r.SuiteSHA256, r.SnapshotSHA256)
	fmt.Fprintf(&b, "通过 %d/%d；脚本模型调用 %d 次；门禁：%t。真实模型/Embedding：%s；Token/费用未测量。\n\n", r.Passed, r.Requests, r.ModelCalls, r.GatePassed, r.ExternalModelStatus)
	fmt.Fprintln(&b, "| 场景 | 结果 | 模型调用 | 工具请求 | 商品查询 | 服务级兜底 | 内部选择兜底 | 重放检查 | 未通过检查 |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, c := range r.Cases {
		var failed []string
		for name, ok := range c.Checks {
			if !ok {
				failed = append(failed, name)
			}
		}
		sort.Strings(failed)
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %t | %t | %s |\n", c.ID, c.Observed.Status, c.Observed.ModelCalls, c.Observed.ToolCalls, c.Observed.ProviderCalls, c.Observed.FallbackCalls, c.Observed.SelectorFallback, c.ReplayChecked, strings.Join(failed, ", "))
	}
	fmt.Fprint(&b, "\n- 真实 Eino ReAct + 业务工具 + Service + InMemory；模型输出和故障按固定脚本注入，每例隔离，不启动文件/MCP/数据库或外部客户端。\n- 工具请求含未知工具等失败尝试，不等于成功执行次数。服务级规则兜底与 Agent 内部未选择兜底分别计数，均不能算模型选品成功。\n- 修复脚本必须收到上一步工具反馈；统计跨 WithTools 克隆共享。完成轮次重放校验完整响应和零新增模型/商品/兜底调用与轮次。\n- 取消在模型执行中主动触发；超时注入 DeadlineExceeded 错误，不是墙钟截止或真实网络超时测试；MaxStep 使用当前锁定 Eino 的图节点步数，不等于模型调用次数。\n- 这是确定性代码容错测试，不是模型自主纠错、语义质量、注入抵抗、生产性能或真实数据库幂等性的证据。\n")
	return b.String()
}
