package flow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/prompt"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/llm"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

func TestRunnerExecutesToolCallsUntilFinalText(t *testing.T) {
	model := &scriptedModel{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_1",
						Name: toolkit.ToolSearchProducts,
						Arguments: mustJSON(t, toolkit.SearchProductsArgs{
							Query:       "study bundle",
							Keywords:    []string{"study"},
							BudgetCents: 300000,
							MaxItems:    3,
						}),
					},
				},
			},
			{FinalText: "final recommendation"},
		},
	}
	runner := NewRunner(model, toolkit.NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector()))

	result, err := runner.Run(context.Background(), RunInput{
		Messages: []prompt.Message{{Role: "user", Content: "study bundle"}},
		Tools:    prompt.NewBuilder().FunctionTools(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.FinalText != "final recommendation" {
		t.Fatalf("final text mismatch: %q", result.FinalText)
	}
	if result.Rounds != 2 {
		t.Fatalf("expected 2 rounds, got %d", result.Rounds)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Name != toolkit.ToolSearchProducts {
		t.Fatalf("unexpected tool results: %+v", result.ToolResults)
	}
	if len(model.requests) != 2 {
		t.Fatalf("expected 2 model requests, got %d", len(model.requests))
	}
	lastRequest := model.requests[1]
	if len(lastRequest.Messages) < 2 {
		t.Fatalf("expected assistant and tool messages appended, got %+v", lastRequest.Messages)
	}
	assistantMessage := lastRequest.Messages[len(lastRequest.Messages)-2]
	if assistantMessage.Role != "assistant" || len(assistantMessage.ToolCalls) != 1 {
		t.Fatalf("expected assistant tool_calls message, got %+v", assistantMessage)
	}
	toolMessage := lastRequest.Messages[len(lastRequest.Messages)-1]
	if toolMessage.Role != "tool" || toolMessage.ToolCallID != "call_1" {
		t.Fatalf("expected tool message with call id, got %+v", toolMessage)
	}
	if !strings.Contains(toolMessage.Content, `"success":true`) {
		t.Fatalf("expected successful tool message, got %s", toolMessage.Content)
	}
}

func TestRunnerReturnsToolErrorsToModel(t *testing.T) {
	model := &scriptedModel{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: "unknown_tool", Arguments: mustJSON(t, map[string]any{})},
				},
			},
			{FinalText: "fallback final"},
		},
	}
	runner := NewRunner(model, toolkit.NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector()))

	result, err := runner.Run(context.Background(), RunInput{
		Messages: []prompt.Message{{Role: "user", Content: "study bundle"}},
		Tools:    prompt.NewBuilder().FunctionTools(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Error == "" {
		t.Fatalf("expected tool error result, got %+v", result.ToolResults)
	}
	lastRequest := model.requests[1]
	if !strings.Contains(lastRequest.Messages[len(lastRequest.Messages)-1].Content, `"success":false`) {
		t.Fatalf("expected failed tool message, got %s", lastRequest.Messages[len(lastRequest.Messages)-1].Content)
	}
}

func TestRunnerUsesFreshExecutorPerRun(t *testing.T) {
	model := &scriptedModel{
		responses: []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call_1",
					Name:      toolkit.ToolSearchProducts,
					Arguments: mustJSON(t, toolkit.SearchProductsArgs{Query: "study", Keywords: []string{"study"}}),
				}},
			},
			{FinalText: "first"},
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call_2",
					Name:      toolkit.ToolSearchProducts,
					Arguments: mustJSON(t, toolkit.SearchProductsArgs{Query: "computer", Keywords: []string{"computer"}}),
				}},
			},
			{FinalText: "second"},
		},
	}

	var created int
	runner := NewRunnerWithFactory(model, func() *toolkit.Executor {
		created++
		return toolkit.NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector())
	})

	for _, query := range []string{"study bundle", "computer bundle"} {
		if _, err := runner.Run(context.Background(), RunInput{
			Messages: []prompt.Message{{Role: "user", Content: query}},
			Tools:    prompt.NewBuilder().FunctionTools(),
		}); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	}

	if created != 2 {
		t.Fatalf("expected fresh executor per run, got %d", created)
	}
}

func TestRunnerStopsAtMaxRounds(t *testing.T) {
	model := &scriptedModel{
		responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{Name: toolkit.ToolSearchProducts, Arguments: mustJSON(t, toolkit.SearchProductsArgs{})}}},
			{ToolCalls: []llm.ToolCall{{Name: toolkit.ToolSearchProducts, Arguments: mustJSON(t, toolkit.SearchProductsArgs{})}}},
		},
	}
	runner := NewRunner(model, toolkit.NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector())).WithMaxRounds(1)

	_, err := runner.Run(context.Background(), RunInput{
		Messages: []prompt.Message{{Role: "user", Content: "study bundle"}},
		Tools:    prompt.NewBuilder().FunctionTools(),
	})
	if err == nil {
		t.Fatal("expected max rounds error")
	}
}

type scriptedModel struct {
	responses []*llm.Response
	requests  []llm.Request
}

func (m *scriptedModel) Name() string {
	return "scripted"
}

func (m *scriptedModel) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.requests = append(m.requests, req)
	if len(m.responses) == 0 {
		return &llm.Response{FinalText: "default final"}, nil
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
}
