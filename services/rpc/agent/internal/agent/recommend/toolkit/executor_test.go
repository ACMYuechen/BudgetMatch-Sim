package toolkit

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

func TestExecutorSearchProducts(t *testing.T) {
	executor := NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector())

	result, err := executor.Execute(context.Background(), ToolSearchProducts, mustArgs(t, SearchProductsArgs{
		Query:       "study bundle",
		Keywords:    []string{"study"},
		BudgetCents: 300000,
		MaxItems:    3,
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	typed, ok := result.Result.(SearchProductsResult)
	if !ok {
		t.Fatalf("unexpected result type %T", result.Result)
	}
	if typed.Count == 0 {
		t.Fatal("expected product candidates")
	}
	if result.Name != ToolSearchProducts || len(result.JSON) == 0 {
		t.Fatalf("unexpected result metadata: %+v", result)
	}
}

func TestExecutorSelectBundleFromStoredCandidates(t *testing.T) {
	executor := NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector())
	searchResult, err := executor.Execute(context.Background(), ToolSearchProducts, mustArgs(t, SearchProductsArgs{
		Query:       "study bundle",
		Keywords:    []string{"study"},
		BudgetCents: 300000,
		MaxItems:    3,
	}))
	if err != nil {
		t.Fatalf("search Execute() error = %v", err)
	}
	products := searchResult.Result.(SearchProductsResult).Products
	ids := make([]string, 0, len(products))
	for _, product := range products {
		ids = append(ids, product.ID)
	}

	bundleResult, err := executor.Execute(context.Background(), ToolSelectBundle, mustArgs(t, SelectBundleArgs{
		CandidateIDs: ids,
		BudgetCents:  300000,
		MaxItems:     3,
	}))
	if err != nil {
		t.Fatalf("select Execute() error = %v", err)
	}

	typed, ok := bundleResult.Result.(SelectBundleResult)
	if !ok {
		t.Fatalf("unexpected result type %T", bundleResult.Result)
	}
	if len(typed.Items) == 0 {
		t.Fatal("expected selected bundle items")
	}
	if typed.TotalPriceCents > 300000 {
		t.Fatalf("total exceeds budget: %d", typed.TotalPriceCents)
	}
}

func TestExecutorRejectsUnknownTool(t *testing.T) {
	executor := NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector())

	if _, err := executor.Execute(context.Background(), "unknown_tool", nil); err == nil {
		t.Fatal("expected unknown tool error")
	}
}

func TestExecutorRejectsMCPWhenDisabled(t *testing.T) {
	executor := NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector())

	_, err := executor.Execute(context.Background(), ToolMCPCallTool, mustArgs(t, MCPCallToolArgs{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello"},
	}))
	if err == nil || !strings.Contains(err.Error(), "mcp is not enabled") {
		t.Fatalf("expected disabled mcp error, got %v", err)
	}
}

func TestExecutorCallsMCPTool(t *testing.T) {
	executor := NewExecutor(tools.NewMockProductProvider(), selector.NewBundleSelector()).WithMCP(fakeMCPConfig(t))

	result, err := executor.Execute(context.Background(), ToolMCPCallTool, mustArgs(t, MCPCallToolArgs{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello mcp"},
	}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	typed, ok := result.Result.(MCPCallToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", result.Result)
	}
	if typed.Name != "echo" || len(typed.Content) == 0 {
		t.Fatalf("unexpected mcp result: %+v", typed)
	}
	if typed.Content[0].Text != "Echo: hello mcp" {
		t.Fatalf("unexpected mcp content: %+v", typed.Content)
	}
}

func mustArgs(t *testing.T, value any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
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
			filepath.Join(filepath.Dir(file), "..", "..", "..", "mcp", "testdata", "fake_mcp_server.py"),
		},
		Timeout: 3000,
	}
}
