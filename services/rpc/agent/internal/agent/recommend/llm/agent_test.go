package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
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

	agent := NewAgent(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{}).WithMaxStep(6)

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
	if len(model.boundTools) != 2 {
		t.Fatalf("expected model to receive 2 tools, got %d", len(model.boundTools))
	}
}

// TestAgentFallsBackWhenModelSkipsSelect 验证模型未调用 select_bundle 直接收尾时，
// Agent 用确定性选择兜底，保证仍返回 grounded 套装。
func TestAgentFallsBackWhenModelSkipsSelect(t *testing.T) {
	model := &scriptedModel{responses: []*schema.Message{
		schema.AssistantMessage("我直接给结论", nil),
	}}

	agent := NewAgent(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpconfig.Config{})

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

// scriptedModel 是测试用模型，按预设顺序返回响应，并记录绑定的工具。
type scriptedModel struct {
	responses  []*schema.Message
	boundTools []*schema.ToolInfo
}

// Generate 按顺序返回预设响应。
func (m *scriptedModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
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
