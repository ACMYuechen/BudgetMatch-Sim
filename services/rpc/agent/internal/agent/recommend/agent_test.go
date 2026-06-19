// Package recommend 提供基于预算与意图的商品推荐 Agent 实现。
// 该包负责解析用户查询意图、搜索候选商品，并从中选择最优商品组合。
package recommend

import (
	"context"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// TestAgent_RunReturnsBundle 验证 Agent 能正确返回一个符合预算的商品组合。
// 测试场景：用户输入中文查询"预算3000，帮我配一套性价比高的学习用品"，
// 期望 Agent 能解析出预算 300000 分（3000 元），并返回总价不超过预算的商品列表。
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

// TestAgent_RunDoesNotProbeMCP 验证推荐 Agent 不会调用 MCP 工具。
// 测试场景：运行 Agent 后检查结果中的 ToolsUsed，确保不包含任何 MCP 相关工具名称。
func TestAgent_RunDoesNotProbeMCP(t *testing.T) {
	agent := NewAgent(tools.NewMockProductProvider(), selector.NewBundleSelector())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := agent.Run(ctx, structInput("study bundle under 3000"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	for _, tool := range result.ToolsUsed {
		if tool.Name == "mcp.demo" || tool.Name == "mcp.echo" {
			t.Fatalf("plain recommend agent should not probe MCP, got %+v", result.ToolsUsed)
		}
	}
}

// structInput 根据查询字符串构造一个 agentcore.Input 结构体，用于测试输入。
func structInput(query string) agentcore.Input {
	return agentcore.Input{
		Query: query,
	}
}
