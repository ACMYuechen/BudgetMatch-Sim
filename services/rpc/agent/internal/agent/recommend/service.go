package recommend

import (
	"context"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
)

// Refiner 定义推荐结果二次精修能力。
// 具体实现可以基于 Eino、规则引擎或其他 Agent 框架，调用方不需要感知底层实现。
type Refiner interface {
	// Enabled 返回当前精修器是否可用。
	Enabled() bool
	// Name 返回精修器名称，用于记录执行轨迹。
	Name() string
	// Refine 根据用户请求和规则推荐草稿生成精修结果。
	Refine(ctx context.Context, input RefineInput) (*RefineResult, error)
}

// RefineInput 表示推荐精修阶段的输入。
type RefineInput struct {
	Query           string                 // Query 用户原始查询
	Intent          agentcore.Intent       // Intent 规则推荐阶段解析出的意图
	SelectedItems   []agentcore.BundleItem // SelectedItems 规则推荐阶段生成的商品草稿
	TotalPriceCents int64                  // TotalPriceCents 草稿商品总价，单位为分
	ToolsUsed       []agentcore.ToolCall   // ToolsUsed 规则推荐阶段已使用的工具记录
}

// RefineResult 表示推荐精修阶段的输出。
type RefineResult struct {
	Summary         string                 // Summary 精修后的推荐摘要
	Items           []agentcore.BundleItem // Items 精修后的商品组合；为空时沿用草稿
	TotalPriceCents int64                  // TotalPriceCents 精修后的商品总价，单位为分
	ToolsUsed       []agentcore.ToolCall   // ToolsUsed 精修阶段新增的工具记录
}

// Service 编排完整推荐流程。
// 它先调用确定性推荐 Agent 生成草稿，再在精修器可用时执行可选二次精修。
type Service struct {
	agent   agentcore.Agent // agent 规则推荐 Agent
	refiner Refiner         // refiner 可选推荐精修器
}

// NewService 创建推荐编排服务。
func NewService(agent agentcore.Agent, refiner Refiner) *Service {
	return &Service{
		agent:   agent,
		refiner: refiner,
	}
}

// Recommend 执行推荐流程，并在精修失败时回退到规则推荐草稿。
func (s *Service) Recommend(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if s == nil || s.agent == nil {
		return nil, agentcore.ErrAgentNotFound
	}

	draft, err := s.agent.Run(ctx, input)
	if err != nil {
		return nil, err
	}
	if !refinerEnabled(s.refiner) {
		return draft, nil
	}

	refined, err := s.refiner.Refine(ctx, refineInput(input.Query, draft))
	if err != nil {
		out := cloneResult(draft)
		out.ToolsUsed = append(out.ToolsUsed, agentcore.ToolCall{
			Name:    "llm." + s.refiner.Name(),
			Success: false,
			Detail:  err.Error(),
		})
		return out, nil
	}

	return mergeRefineResult(draft, refined, s.refiner.Name()), nil
}

// refinerEnabled 判断精修器是否存在且可用。
func refinerEnabled(refiner Refiner) bool {
	return refiner != nil && refiner.Enabled()
}

// refineInput 将规则推荐草稿转换为精修输入。
func refineInput(query string, draft *agentcore.Result) RefineInput {
	if draft == nil {
		return RefineInput{Query: query}
	}
	return RefineInput{
		Query:           query,
		Intent:          draft.Intent,
		SelectedItems:   append([]agentcore.BundleItem(nil), draft.Items...),
		TotalPriceCents: draft.TotalPriceCents,
		ToolsUsed:       append([]agentcore.ToolCall(nil), draft.ToolsUsed...),
	}
}

// mergeRefineResult 将精修结果合并回规则推荐草稿。
func mergeRefineResult(draft *agentcore.Result, refined *RefineResult, refinerName string) *agentcore.Result {
	out := cloneResult(draft)
	if refined == nil {
		return out
	}

	if refined.Summary != "" {
		out.Summary = refined.Summary
	}
	if len(refined.Items) > 0 {
		out.Items = append([]agentcore.BundleItem(nil), refined.Items...)
		out.TotalPriceCents = refined.TotalPriceCents
	}

	out.ToolsUsed = append(out.ToolsUsed, agentcore.ToolCall{
		Name:    "llm." + refinerName,
		Success: true,
		Detail:  "completed recommend refinement",
	})
	out.ToolsUsed = append(out.ToolsUsed, refined.ToolsUsed...)
	return out
}

// cloneResult 深拷贝推荐结果，避免精修流程修改草稿对象。
func cloneResult(result *agentcore.Result) *agentcore.Result {
	if result == nil {
		return &agentcore.Result{}
	}
	return &agentcore.Result{
		Intent: agentcore.Intent{
			BudgetCents: result.Intent.BudgetCents,
			MaxItems:    result.Intent.MaxItems,
			Keywords:    append([]string(nil), result.Intent.Keywords...),
			Preferences: append([]string(nil), result.Intent.Preferences...),
		},
		Items:           append([]agentcore.BundleItem(nil), result.Items...),
		TotalPriceCents: result.TotalPriceCents,
		Summary:         result.Summary,
		ToolsUsed:       append([]agentcore.ToolCall(nil), result.ToolsUsed...),
	}
}
