package recommend

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
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

func TestAgent_RunProbesMCPWhenEnabled(t *testing.T) {
	agent := NewAgent(tools.NewMockProductProvider(), selector.NewBundleSelector(), fakeMCPConfig(t))

	result, err := agent.Run(context.Background(), structInput("study bundle under 3000"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var found bool
	for _, tool := range result.ToolsUsed {
		if tool.Name == "mcp.echo" {
			found = true
			if !tool.Success {
				t.Fatalf("expected successful mcp probe, got %+v", tool)
			}
			if !strings.Contains(tool.Detail, "Echo: budgetmatch agent mcp probe") {
				t.Fatalf("unexpected mcp detail: %q", tool.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("expected mcp tool call in result, got %+v", result.ToolsUsed)
	}
}

func structInput(query string) agentcore.Input {
	return agentcore.Input{
		Query: query,
	}
}

func fakeMCPConfig(t *testing.T) mcp.Config {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return mcp.Config{
		Enabled: true,
		Command: "python3",
		Args: []string{
			filepath.Join(filepath.Dir(file), "..", "..", "mcp", "testdata", "fake_mcp_server.py"),
		},
		Timeout: 3000,
	}
}
