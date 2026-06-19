// Package eino 提供基于 Eino 框架的 LLM 模型封装、Prompt 构建、工具定义与 ReAct Agent 运行器。
package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// TestRunnerExecutesEinoToolCalls 验证 Runner 能正确执行 search_products 和 select_bundle 工具调用，
// 并返回预期的最终文本与工具结果。
func TestRunnerExecutesEinoToolCalls(t *testing.T) {
	model := &scriptedModel{
		responses: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: schema.FunctionCall{
					Name: toolkit.ToolSearchProducts,
					Arguments: mustJSONString(t, toolkit.SearchProductsArgs{
						Query:       "study bundle",
						Keywords:    []string{"study"},
						BudgetCents: 300000,
						MaxItems:    3,
					}),
				},
			}}),
			schema.AssistantMessage("", []schema.ToolCall{{
				ID:   "call_2",
				Type: "function",
				Function: schema.FunctionCall{
					Name: toolkit.ToolSelectBundle,
					Arguments: mustJSONString(t, toolkit.SelectBundleArgs{
						BudgetCents: 300000,
						MaxItems:    3,
					}),
				},
			}}),
			schema.AssistantMessage("最终推荐结果", nil),
		},
	}
	runner := NewRunner(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpDisabled()).WithMaxStep(6)

	result, err := runner.Run(context.Background(), RunInput{
		Query: "study bundle",
		Intent: agentcore.Intent{
			BudgetCents: 300000,
			MaxItems:    3,
			Keywords:    []string{"study"},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.FinalText != "最终推荐结果" {
		t.Fatalf("unexpected final text %q", result.FinalText)
	}
	if len(result.ToolResults) != 2 {
		t.Fatalf("expected 2 tool results, got %+v", result.ToolResults)
	}
	if result.ToolResults[0].Name != toolkit.ToolSearchProducts || len(result.ToolResults[0].JSON) == 0 {
		t.Fatalf("unexpected search tool result: %+v", result.ToolResults[0])
	}
	if result.ToolResults[1].Name != toolkit.ToolSelectBundle || len(result.ToolResults[1].JSON) == 0 {
		t.Fatalf("unexpected select tool result: %+v", result.ToolResults[1])
	}
	if len(model.boundTools) != 3 {
		t.Fatalf("expected model to receive 3 tools, got %d", len(model.boundTools))
	}
}

// TestRunnerCollectsToolErrors 验证 Runner 在 MCP 工具调用失败时，
// 能正确收集并返回错误信息（如 "mcp is not enabled"）。
func TestRunnerCollectsToolErrors(t *testing.T) {
	model := &scriptedModel{
		responses: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: schema.FunctionCall{
					Name:      toolkit.ToolMCPCallTool,
					Arguments: mustJSONString(t, toolkit.MCPCallToolArgs{Name: "echo", Arguments: map[string]any{"message": "hi"}}),
				},
			}}),
			schema.AssistantMessage("降级输出", nil),
		},
	}
	runner := NewRunner(model, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcpDisabled()).WithMaxStep(4)

	result, err := runner.Run(context.Background(), RunInput{Query: "call mcp"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.ToolResults) != 1 || result.ToolResults[0].Error == "" {
		t.Fatalf("expected collected tool error, got %+v", result.ToolResults)
	}
	if !strings.Contains(result.ToolResults[0].Error, "mcp is not enabled") {
		t.Fatalf("unexpected tool error %q", result.ToolResults[0].Error)
	}
}

// scriptedModel 是一个用于测试的模拟模型，按预设顺序返回响应，并记录收到的请求和绑定的工具。
type scriptedModel struct {
	responses  []*schema.Message   // 预设的响应消息队列
	requests   [][]*schema.Message // 记录每次 Generate 收到的请求
	boundTools []*schema.ToolInfo  // 记录 WithTools 绑定的工具
}

// Generate 模拟模型生成：检查上下文、记录请求并按顺序返回预设响应。
func (m *scriptedModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.requests = append(m.requests, input)
	if len(m.responses) == 0 {
		return schema.AssistantMessage("default final", nil), nil
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

// Stream 将 Generate 结果包装为流式读取器（伪流式）。
func (m *scriptedModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// WithTools 记录绑定的工具并返回模型的浅拷贝副本。
func (m *scriptedModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	m.boundTools = append([]*schema.ToolInfo(nil), tools...)
	clone := *m
	return &clone, nil
}

// mustJSONString 将任意值序列化为 JSON 字符串，失败时终止测试。
func mustJSONString(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(data)
}

// mcpDisabled 返回一个未启用 MCP 的配置（用于测试）。
func mcpDisabled() mcp.Config {
	return mcp.Config{}
}
