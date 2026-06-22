package recommend

import (
	"context"
	"errors"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
)

// TestServiceReturnsDraftWhenRefinerDisabled 验证精修器不可用时，服务直接返回规则推荐草稿。
func TestServiceReturnsDraftWhenRefinerDisabled(t *testing.T) {
	draft := &agentcore.Result{Summary: "draft"}
	service := NewService(&stubAgent{result: draft}, nil)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "draft" {
		t.Fatalf("expected draft summary, got %q", result.Summary)
	}
}

// TestServiceMergesRefineResult 验证精修成功时，服务会合并摘要、商品结果和工具记录。
func TestServiceMergesRefineResult(t *testing.T) {
	draft := &agentcore.Result{
		Summary: "draft",
		Items: []agentcore.BundleItem{{
			ID: "draft-item",
		}},
		TotalPriceCents: 100,
	}
	refined := &RefineResult{
		Summary: "refined",
		Items: []agentcore.BundleItem{{
			ID: "refined-item",
		}},
		TotalPriceCents: 200,
		ToolsUsed: []agentcore.ToolCall{{
			Name:    "tool.select_bundle",
			Success: true,
			Detail:  "ok",
		}},
	}
	service := NewService(&stubAgent{result: draft}, &stubRefiner{name: "eino.openai", enabled: true, result: refined})

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "refined" {
		t.Fatalf("expected refined summary, got %q", result.Summary)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "refined-item" {
		t.Fatalf("unexpected refined items: %+v", result.Items)
	}
	if result.TotalPriceCents != 200 {
		t.Fatalf("unexpected total price: %d", result.TotalPriceCents)
	}
	if len(result.ToolsUsed) != 2 || result.ToolsUsed[0].Name != "llm.eino.openai" {
		t.Fatalf("unexpected tools used: %+v", result.ToolsUsed)
	}
}

// TestServiceFallsBackWhenRefinerFails 验证精修失败时，服务回退到草稿并记录失败工具。
func TestServiceFallsBackWhenRefinerFails(t *testing.T) {
	draft := &agentcore.Result{Summary: "draft"}
	service := NewService(&stubAgent{result: draft}, &stubRefiner{name: "eino.openai", enabled: true, err: errors.New("model down")})

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "draft" {
		t.Fatalf("expected fallback draft, got %q", result.Summary)
	}
	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0].Success || result.ToolsUsed[0].Detail != "model down" {
		t.Fatalf("unexpected fallback tool record: %+v", result.ToolsUsed)
	}
}

// stubAgent 是用于测试的规则推荐 Agent。
type stubAgent struct {
	result *agentcore.Result // result 预置推荐结果
	err    error             // err 预置错误
}

// Name 返回测试 Agent 名称。
func (a *stubAgent) Name() string {
	return "stub_agent"
}

// Run 返回预置推荐结果或错误。
func (a *stubAgent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.err != nil {
		return nil, a.err
	}
	return a.result, nil
}

// stubRefiner 是用于测试的推荐精修器。
type stubRefiner struct {
	name    string        // name 精修器名称
	enabled bool          // enabled 是否启用
	result  *RefineResult // result 预置精修结果
	err     error         // err 预置错误
}

// Enabled 返回测试精修器是否启用。
func (r *stubRefiner) Enabled() bool {
	return r.enabled
}

// Name 返回测试精修器名称。
func (r *stubRefiner) Name() string {
	return r.name
}

// Refine 返回预置精修结果或错误。
func (r *stubRefiner) Refine(ctx context.Context, input RefineInput) (*RefineResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
}
