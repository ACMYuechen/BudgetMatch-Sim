package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TestAgentDrivesReactToolCalls 验证 Agent 让 ReAct 真正编排 search_products + select_bundle，
// 并从工具结果回填出 grounded 的商品套装与工具记录。
func TestAgentDrivesReactToolCalls(t *testing.T) {
	model := &scriptedModel{responses: []*schema.Message{
		toolCallMessage("call_1", toolSearchProducts, searchArgs{
			Query: "study bundle", Keywords: []string{"study"}, BudgetCents: 300000, MaxItems: 3,
		}),
		toolCallMessage("call_2", toolSelectBundle, selectArgs{BudgetCents: 300000, MaxItems: 3}),
		schema.AssistantMessage("最终推荐结果", nil),
	}}

	agent := NewAgent(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{}).WithMaxStep(6)

	result, err := agent.Run(context.Background(), agentcore.Input{Query: "study bundle", BudgetCents: 300000, MaxItems: 3})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Summary != "最终推荐结果" {
		t.Fatalf("unexpected summary %q", result.Summary)
	}
	if len(result.Items) == 0 {
		t.Fatalf("expected grounded bundle items, got %+v", result)
	}
	if result.TotalPriceCents > 300000 {
		t.Fatalf("total exceeds budget: %d", result.TotalPriceCents)
	}
	// ToolsUsed[0] 是 llm 编排标记，随后是工具调用记录。
	if len(result.ToolsUsed) != 3 {
		t.Fatalf("expected 3 tool records, got %+v", result.ToolsUsed)
	}
	if !strings.HasPrefix(result.ToolsUsed[0].Name, "llm.") {
		t.Fatalf("expected llm orchestration marker, got %+v", result.ToolsUsed[0])
	}
	if result.ToolsUsed[1].Name != "tool."+toolSearchProducts || !result.ToolsUsed[1].Success {
		t.Fatalf("unexpected search record: %+v", result.ToolsUsed[1])
	}
	if result.ToolsUsed[2].Name != "tool."+toolSelectBundle || !result.ToolsUsed[2].Success {
		t.Fatalf("unexpected select record: %+v", result.ToolsUsed[2])
	}
	if len(model.boundTools) != 4 {
		t.Fatalf("expected model to receive 4 tools, got %d", len(model.boundTools))
	}
}

// TestAgentFallsBackWhenModelSkipsSelect 验证模型未调用 select_bundle 直接收尾时，
// Agent 用确定性选择兜底，保证仍返回 grounded 套装。
func TestAgentFallsBackWhenModelSkipsSelect(t *testing.T) {
	model := &scriptedModel{responses: []*schema.Message{
		schema.AssistantMessage("我直接给结论", nil),
	}}

	agent := NewAgent(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{})

	result, err := agent.Run(context.Background(), agentcore.Input{Query: "预算3000 学习用品"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatalf("expected deterministic fallback bundle, got %+v", result)
	}
	var sawFallback bool
	for _, call := range result.ToolsUsed {
		if call.Name == "selector.fallback" {
			sawFallback = true
		}
	}
	if !sawFallback {
		t.Fatalf("expected selector.fallback record, got %+v", result.ToolsUsed)
	}
}

// TestAgentInjectsHistoryIntoPrompt 验证多轮对话时模型收到 [system, ...history, currentUser]，
// 且历史中的 user 消息是裸 Query（意图脚手架只出现在当前轮）。
func TestAgentInjectsHistoryIntoPrompt(t *testing.T) {
	ctx := context.Background()
	var received [][]*schema.Message
	model := &scriptedModel{
		responses: []*schema.Message{schema.AssistantMessage("第二轮推荐", nil)},
		received:  &received,
	}

	mem := memory.NewInMemory(memory.Conf{})
	if err := mem.Append(ctx, "u1", "c1",
		schema.UserMessage("预算3000买键盘"),
		schema.AssistantMessage("已推荐入门机械键盘", nil)); err != nil {
		t.Fatalf("seed memory error = %v", err)
	}

	agent := NewAgent(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{}).
		WithMemory(mem, 20)

	if _, err := agent.Run(ctx, agentcore.Input{Query: "预算加到5000", UserId: "u1", ConversationId: "c1"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(received) == 0 {
		t.Fatal("model received no messages")
	}
	first := received[0]
	if len(first) != 4 {
		t.Fatalf("expected [system, user, assistant, user], got %d messages", len(first))
	}
	if first[0].Role != schema.System {
		t.Fatalf("expected system message first, got %+v", first[0])
	}
	if first[1].Role != schema.User || first[1].Content != "预算3000买键盘" {
		t.Fatalf("expected raw historical query without scaffolding, got %+v", first[1])
	}
	if first[2].Role != schema.Assistant || first[2].Content != "已推荐入门机械键盘" {
		t.Fatalf("unexpected historical assistant message: %+v", first[2])
	}
	if first[3].Role != schema.User ||
		!strings.Contains(first[3].Content, "预算加到5000") ||
		!strings.Contains(first[3].Content, "Parsed intent") {
		t.Fatalf("expected current user message with intent scaffolding, got %+v", first[3])
	}
}

// TestAgentToleratesHistoryFailure 验证记忆读取失败时降级为单轮推荐，不阻断请求。
func TestAgentToleratesHistoryFailure(t *testing.T) {
	model := &scriptedModel{responses: []*schema.Message{schema.AssistantMessage("ok", nil)}}
	agent := NewAgent(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}, filetools.Config{}).
		WithMemory(&failingMemory{}, 20)

	result, err := agent.Run(context.Background(), agentcore.Input{Query: "预算3000 学习用品", UserId: "u1", ConversationId: "c1"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Summary != "ok" {
		t.Fatalf("expected run to succeed without history, got %+v", result)
	}
}

// failingMemory 是恒定报错的记忆实现，用于容错测试。
type failingMemory struct{}

func (f *failingMemory) Append(ctx context.Context, userId, conversationId string, msgs ...*schema.Message) error {
	return errors.New("memory down")
}

func (f *failingMemory) History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error) {
	return nil, errors.New("memory down")
}

func (f *failingMemory) Clear(ctx context.Context, userId, conversationId string) error {
	return errors.New("memory down")
}

// scriptedModel 是测试用模型，按预设顺序返回响应，并记录绑定的工具与收到的消息。
// received 用指针共享：ReAct 内部通过 WithTools 的克隆调用 Generate，记录需要跨克隆可见。
type scriptedModel struct {
	responses  []*schema.Message
	boundTools []*schema.ToolInfo
	received   *[][]*schema.Message
}

// Generate 按顺序返回预设响应，并记录本次收到的完整消息列表。
func (m *scriptedModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.received != nil {
		*m.received = append(*m.received, input)
	}
	if len(m.responses) == 0 {
		return schema.AssistantMessage("default final", nil), nil
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

// Stream 把 Generate 结果包成流，满足 BaseChatModel 接口。
func (m *scriptedModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// WithTools 记录绑定的工具并返回模型副本。
func (m *scriptedModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.boundTools = append([]*schema.ToolInfo(nil), tools...)
	clone := *m
	return &clone, nil
}

// toolCallMessage 构造一条携带单个工具调用的 assistant 消息。
func toolCallMessage(id, name string, args any) *schema.Message {
	data, _ := json.Marshal(args)
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID:   id,
		Type: "function",
		Function: schema.FunctionCall{
			Name:      name,
			Arguments: string(data),
		},
	}})
}
