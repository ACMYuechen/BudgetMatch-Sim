package recommend

import (
	"context"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

func TestAgent_RunReturnsBundle(t *testing.T) {
	agent := NewAgent(tools.NewMockProductProvider(), selector.NewBundleSelector())

	result, err := agent.Run(context.Background(), structInput("预算3000，帮我配一套性价比高的学习用品"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Intent.BudgetCents != 300000 {
		t.Fatalf("budget mismatch, got %d", result.Intent.BudgetCents)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected bundle items")
	}
	if result.TotalPriceCents > result.Intent.BudgetCents {
		t.Fatalf("total exceeds budget, total=%d budget=%d", result.TotalPriceCents, result.Intent.BudgetCents)
	}
	if len(result.ToolsUsed) == 0 || !result.ToolsUsed[0].Success {
		t.Fatalf("expected successful tool call, got %+v", result.ToolsUsed)
	}
}

func structInput(query string) agentcore.Input {
	return agentcore.Input{
		Query: query,
	}
}
