// Package recommend 提供基于预算与意图的商品推荐 Agent 实现。
// 该包负责解析用户查询意图、搜索候选商品，并从中选择最优商品组合。
package recommend

import (
	"context"
	"fmt"
	"strings"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	"github.com/cloudwego/eino/schema"
	"github.com/zeromicro/go-zero/core/logx"
)

// AgentName 推荐 Agent 的唯一标识名称。
const AgentName = "recommend_agent"

// Agent 推荐 Agent，负责根据用户输入的预算与需求，生成最优商品组合。
type Agent struct {
	planner  *Planner                 // planner 意图解析器，用于从用户查询中提取预算、关键词等意图信息
	provider tools.ProductProvider    // provider 商品数据源，提供候选商品搜索能力
	selector *selector.BundleSelector // selector 商品选择器，从候选商品中挑选最优组合
	memory   memory.Manager           // memory 会话记忆，用于规则兜底继承历史约束
	window   int                      // window 最多读取的历史消息数
}

// WithMemory 启用规则推荐的多轮上下文读取。
func (a *Agent) WithMemory(mem memory.Manager, window int) *Agent {
	a.memory = mem
	a.window = window
	return a
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	historyQueries := a.loadHistoryQueries(ctx, input)
	intent, err := a.planner.Resolve(input, historyQueries)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	search, err := tools.SearchWithTrace(ctx, a.provider, tools.SearchProductsReq{
		Query:       input.Query,
		Keywords:    intent.Keywords,
		BudgetCents: intent.BudgetCents,
		MaxItems:    intent.MaxItems,
	})
	if err != nil {
		return nil, err
	}
	candidates := search.Candidates

	items, total := a.selector.Select(candidates, intent)
	toolsUsed := []agentcore.ToolCall{
		{Name: safety.Label(a.provider.Name()), Success: true, Detail: fmt.Sprintf("loaded %d candidates", len(candidates))},
	}
	toolsUsed = append(toolsUsed, safety.ToolCalls(search.Calls)...)

	result := &agentcore.Result{
		Candidates:      candidates,
		Intent:          intent,
		Items:           items,
		TotalPriceCents: total,
		Summary:         agentcore.BundleSummary(len(items), total, intent.BudgetCents),
		ToolsUsed:       toolsUsed,
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	constraints, _ := agentcore.NewConstraints(intent)
	if err := constraints.ValidateResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

// loadHistoryQueries 提取历史中的用户原始问题；读取失败时降级为单轮规则推荐。
func (a *Agent) loadHistoryQueries(ctx context.Context, input agentcore.Input) []string {
	if a.memory == nil || input.UserId == "" || input.ConversationId == "" {
		return nil
	}
	history, err := a.memory.History(ctx, input.UserId, input.ConversationId, a.window)
	if err != nil {
		logx.WithContext(ctx).Errorw("load conversation history for fallback failed",
			logx.Field("conversation_id", safety.Label(input.ConversationId)),
			logx.Field("error_code", safety.ErrorCode(err)),
		)
		return nil
	}

	queries := make([]string, 0, len(history)/2)
	for _, msg := range history {
		if msg != nil && msg.Role == schema.User && strings.TrimSpace(msg.Content) != "" {
			queries = append(queries, msg.Content)
		}
	}
	return queries
}
