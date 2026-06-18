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

type scriptedModel struct {
	responses  []*schema.Message
	requests   [][]*schema.Message
	boundTools []*schema.ToolInfo
}

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

func (m *scriptedModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *scriptedModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	m.boundTools = append([]*schema.ToolInfo(nil), tools...)
	clone := *m
	return &clone, nil
}

func mustJSONString(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(data)
}

func mcpDisabled() mcp.Config {
	return mcp.Config{}
}
