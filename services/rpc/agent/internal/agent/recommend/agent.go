// Package recommend 提供基于预算与意图的商品推荐 Agent 实现。
// 该包负责解析用户查询意图、搜索候选商品，并从中选择最优商品组合。
package recommend

import (
	"context"
	"fmt"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// AgentName 推荐 Agent 的唯一标识名称。
const AgentName = "recommend_agent"

// Agent 推荐 Agent，负责根据用户输入的预算与需求，生成最优商品组合。
type Agent struct {
	planner  *Planner                 // planner 意图解析器，用于从用户查询中提取预算、关键词等意图信息
	provider tools.ProductProvider    // provider 商品数据源，提供候选商品搜索能力
	selector *selector.BundleSelector // selector 商品选择器，从候选商品中挑选最优组合
}

// NewAgent 创建一个新的推荐 Agent 实例。
// provider 为商品搜索提供者，selector 为商品组合选择器。
func NewAgent(provider tools.ProductProvider, selector *selector.BundleSelector) *Agent {
	return &Agent{
		planner:  NewPlanner(),
		provider: provider,
		selector: selector,
	}
}

// Name 返回当前 Agent 的名称标识。
func (a *Agent) Name() string {
	return AgentName
}

// Run 执行推荐流程，返回选中的商品组合结果。
// 流程分为三步：
//  1. 意图解析：通过 planner 从用户输入中提取预算、关键词、偏好等意图；
//  2. 候选搜索：调用 provider 根据意图关键词和预算搜索候选商品；
//  3. 商品选择：通过 selector 从候选商品中按评分选出最优组合，确保总价不超出预算。
func (a *Agent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	intent := a.planner.Parse(input)
	candidates, err := a.provider.SearchProducts(ctx, tools.SearchProductsReq{
		Query:       input.Query,
		Keywords:    intent.Keywords,
		BudgetCents: intent.BudgetCents,
		MaxItems:    intent.MaxItems,
	})
	if err != nil {
		return nil, err
	}

	items, total := a.selector.Select(candidates, intent)
	toolsUsed := []agentcore.ToolCall{
		{Name: a.provider.Name(), Success: true, Detail: fmt.Sprintf("loaded %d candidates", len(candidates))},
	}

	return &agentcore.Result{
		Intent:          intent,
		Items:           items,
		TotalPriceCents: total,
		Summary:         summary(len(items), total, intent.BudgetCents),
		ToolsUsed:       toolsUsed,
	}, nil
}

// summary 根据选中的商品数量、总价和预算生成结果摘要文本。
func summary(count int, total, budget int64) string {
	if count == 0 {
		return "No bundle was found within the current budget."
	}
	if budget <= 0 {
		return fmt.Sprintf("Selected %d items with total price %.2f.", count, float64(total)/100)
	}
	return fmt.Sprintf("Selected %d items with total price %.2f, within budget %.2f.", count, float64(total)/100, float64(budget)/100)
}
