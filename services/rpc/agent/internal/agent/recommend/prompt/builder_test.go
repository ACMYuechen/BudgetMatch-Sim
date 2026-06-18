package prompt

import (
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

func TestBuilderBuildsPromptMessages(t *testing.T) {
	builder := NewBuilder()

	messages := builder.Build(BuildInput{
		Query: "预算3000，帮我配一套学习用品",
		Intent: agentcore.Intent{
			BudgetCents: 300000,
			MaxItems:    3,
			Keywords:    []string{"学习"},
			Preferences: []string{"性价比"},
		},
		Candidates: []tools.ProductCandidate{
			{ID: "p1", Name: "Desk Lamp", Category: "study", Source: "mock", PriceCents: 16900, Stock: 10, Sold: 100, Tags: []string{"study"}},
		},
		SelectedItems: []agentcore.BundleItem{
			{ID: "p1", Name: "Desk Lamp", Category: "study", Source: "mock", PriceCents: 16900, Stock: 10, Score: 12.3, Reason: "value pick"},
		},
		TotalPriceCents: 16900,
		ToolsUsed: []agentcore.ToolCall{
			{Name: "mock.product_provider", Success: true, Detail: "loaded 1 mock candidates"},
		},
	})

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "recommend_agent") {
		t.Fatalf("unexpected system message: %+v", messages[0])
	}
	userPrompt := messages[1].Content
	for _, want := range []string{
		"User request",
		"Parsed intent",
		"Available product candidates",
		"Current deterministic bundle draft",
		"Tools already used",
		"Desk Lamp",
		"mock.product_provider",
	} {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("expected user prompt to contain %q, got %s", want, userPrompt)
		}
	}
}

func TestBuilderReturnsFunctionToolSchemas(t *testing.T) {
	tools := NewBuilder().FunctionTools()

	if len(tools) != 3 {
		t.Fatalf("expected 3 tool schemas, got %d", len(tools))
	}
	if tools[0].Name != "search_products" || tools[1].Name != "select_bundle" || tools[2].Name != "mcp_call_tool" {
		t.Fatalf("unexpected tool schemas: %+v", tools)
	}
	for i, tool := range tools {
		if tool.Type != "function" {
			t.Fatalf("expected function tool, got %+v", tool)
		}
		if i < 2 && !tool.Strict {
			t.Fatalf("expected strict schema for %s", tool.Name)
		}
		if tool.Name == "mcp_call_tool" && tool.Strict {
			t.Fatalf("mcp_call_tool should allow dynamic MCP arguments")
		}
		if tool.Parameters["additionalProperties"] != false {
			t.Fatalf("expected additionalProperties=false for %s", tool.Name)
		}
	}
}
