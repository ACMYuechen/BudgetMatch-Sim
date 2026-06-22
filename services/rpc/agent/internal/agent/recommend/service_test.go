package recommend

import (
	"context"
	"errors"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
)

// TestServiceUsesFallbackWhenNoPrimary 验证未配置 primary 时，服务直接使用规则兜底 Agent。
func TestServiceUsesFallbackWhenNoPrimary(t *testing.T) {
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}
	service := NewService(fallback, nil)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "rule" {
		t.Fatalf("expected fallback summary, got %q", result.Summary)
	}
}

// TestServicePrefersPrimary 验证 primary 可用时优先返回其结果。
func TestServicePrefersPrimary(t *testing.T) {
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}
	primary := &stubAgent{name: "llm", result: &agentcore.Result{Summary: "react"}}
	service := NewService(fallback, primary)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "react" {
		t.Fatalf("expected primary summary, got %q", result.Summary)
	}
}

// TestServiceFallsBackWhenPrimaryFails 验证 primary 失败时降级到兜底，并记录失败原因。
func TestServiceFallsBackWhenPrimaryFails(t *testing.T) {
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}
	primary := &stubAgent{name: "llm", err: errors.New("model down")}
	service := NewService(fallback, primary)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "rule" {
		t.Fatalf("expected fallback summary, got %q", result.Summary)
	}
	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0].Success {
		t.Fatalf("expected failed primary record, got %+v", result.ToolsUsed)
	}
	if result.ToolsUsed[0].Name != "primary.llm" || result.ToolsUsed[0].Detail != "model down" {
		t.Fatalf("unexpected fallback tool record: %+v", result.ToolsUsed[0])
	}
}

// stubAgent 是用于测试的 agentcore.Agent 实现。
type stubAgent struct {
	name   string
	result *agentcore.Result
	err    error
}

// Name 返回测试 Agent 名称。
func (a *stubAgent) Name() string { return a.name }

// Run 返回预置结果或错误。
func (a *stubAgent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.err != nil {
		return nil, a.err
	}
	// 返回副本，避免测试间共享同一结果指针被追加修改。
	clone := *a.result
	clone.ToolsUsed = append([]agentcore.ToolCall(nil), a.result.ToolsUsed...)
	return &clone, nil
}
