package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/einolog"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	einoagent "github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/zeromicro/go-zero/core/logx"
)

// logCallbacks 是全局复用的 Eino 组件日志回调：
// 挂在每次 Generate 上，模型/工具以及工具内部触发的检索、嵌入组件都会被同一 handler 记录。
var logCallbacks = einolog.NewHandler()

// AgentName 是 LLM 推荐 Agent 的名称标识。
const AgentName = "recommend_agent.llm"

// defaultMaxStep 是 ReAct Agent 的默认最大步数。
const defaultMaxStep = 8

// Agent 是基于 Eino ReAct 的推荐 Agent，实现 agentcore.Agent 接口。
//
// 它是 LLM 链路的唯一编排入口：模型自己决定调用 search_products / select_bundle / MCP 工具的顺序，
// 结构化商品结果由工具回填到 session，避免模型编造商品、价格与库存。
// 模型未产出套装时回落到确定性选择，保证响应始终 grounded。
type Agent struct {
	model            model.ToolCallingChatModel
	planner          *recommendagent.Planner
	provider         tools.ProductProvider
	selector         *selector.BundleSelector
	mcpCfg           mcpconfig.Config
	fileTools        *filetools.Workspace
	maxStep          int
	memory           memory.Manager // memory 会话记忆，只读取历史；写入统一由 Service 层完成
	maxHistory       int            // maxHistory 单次读取的最大历史条数
	maxContextTokens int            // maxContextTokens 发送给模型的消息上下文近似 token 上限
}

// 确保 Agent 实现 agentcore.Agent。
var _ agentcore.Agent = (*Agent)(nil)

// NewAgent 创建基于 Eino ReAct 的推荐 Agent。
func NewAgent(m model.ToolCallingChatModel, provider tools.ProductProvider, sel *selector.BundleSelector,
	mcpCfg mcpconfig.Config, fileCfg filetools.Config) *Agent {
	workspace, err := filetools.NewWorkspace(fileCfg)
	if err != nil {
		panic(err)
	}
	return &Agent{
		model:            m,
		planner:          recommendagent.NewPlanner(),
		provider:         provider,
		selector:         sel,
		mcpCfg:           mcpCfg,
		fileTools:        workspace,
		maxStep:          defaultMaxStep,
		maxContextTokens: memory.Conf{}.ContextTokens(),
	}
}

// WithMaxStep 设置 ReAct 最大步数，仅接受正值。
func (a *Agent) WithMaxStep(maxStep int) *Agent {
	if maxStep > 0 {
		a.maxStep = maxStep
	}
	return a
}

// WithMemory 启用会话记忆读取：每次 Run 前拉取最近 maxHistory 条历史注入模型输入。
func (a *Agent) WithMemory(mem memory.Manager, maxHistory int) *Agent {
	a.memory = mem
	a.maxHistory = maxHistory
	return a
}

// WithMaxContextTokens 设置发送给模型的消息上下文近似 token 上限，仅接受正值。
func (a *Agent) WithMaxContextTokens(maxContextTokens int) *Agent {
	if maxContextTokens > 0 {
		a.maxContextTokens = maxContextTokens
	}
	return a
}

// Name 返回 Agent 名称。
func (a *Agent) Name() string {
	return AgentName
}

// Run 执行一次完整的 ReAct 推荐流程。
func (a *Agent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if a == nil || a.model == nil {
		return nil, errors.New("llm chat model is not configured")
	}
	if a.provider == nil || a.selector == nil {
		return nil, errors.New("product provider and bundle selector are required")
	}

	intent := a.planner.Parse(input)
	s := newSession(a.provider, a.selector, intent)

	reactTools, err := businessTools(s, a.fileTools)
	if err != nil {
		return nil, err
	}
	mcpToolList, cleanup, err := mcpTools(ctx, a.mcpCfg, s)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	reactTools = append(reactTools, mcpToolList...)

	reactAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: a.model,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: reactTools},
		MaxStep:          a.maxStep,
	})
	if err != nil {
		return nil, fmt.Errorf("build react agent: %w", err)
	}

	history := a.loadHistory(ctx, input)
	messages := buildMessages(input, intent, history, a.maxContextTokens)
	keptHistory := max(len(messages)-2, 0)
	if keptHistory < len(history) {
		logx.WithContext(ctx).Infow("conversation history trimmed by context token budget",
			logx.Field("conversation_id", input.ConversationId),
			logx.Field("loaded_messages", len(history)),
			logx.Field("kept_messages", keptHistory),
			logx.Field("max_context_tokens", a.maxContextTokens),
		)
	}

	final, err := reactAgent.Generate(ctx, messages,
		einoagent.WithComposeOptions(compose.WithCallbacks(logCallbacks)))
	if err != nil {
		return nil, err
	}

	var finalText string
	if final != nil {
		finalText = final.Content
	}
	return a.assemble(ctx, input, intent, s, finalText), nil
}

// loadHistory 读取会话历史。记忆未启用或读取失败时返回空——
// 历史是增强信息而非必需输入，读取失败降级为单轮推荐，不阻断请求。
func (a *Agent) loadHistory(ctx context.Context, input agentcore.Input) []*schema.Message {
	if a.memory == nil || input.ConversationId == "" {
		return nil
	}
	history, err := a.memory.History(ctx, input.UserId, input.ConversationId, a.maxHistory)
	if err != nil {
		logx.WithContext(ctx).Errorw("load conversation history failed",
			logx.Field("conversation_id", input.ConversationId),
			logx.Field("error", err.Error()),
		)
		return nil
	}
	return history
}

// assemble 把 session 中累积的类型化结果组装为业务响应。
func (a *Agent) assemble(ctx context.Context, input agentcore.Input, intent agentcore.Intent, s *session, finalText string) *agentcore.Result {
	items, total, calls := s.snapshot()
	if len(items) == 0 {
		var items2Err error
		items, total, items2Err = a.fallbackSelect(ctx, input, intent, s)
		detail := "model produced no bundle; used deterministic selection"
		if items2Err != nil {
			detail = "deterministic fallback failed: " + items2Err.Error()
		}
		calls = append(calls, agentcore.ToolCall{
			Name:    "selector.fallback",
			Success: len(items) > 0,
			Detail:  detail,
		})
	}

	summaryText := strings.TrimSpace(finalText)
	if summaryText == "" {
		summaryText = deterministicSummary(len(items), total, intent.BudgetCents)
	}

	toolsUsed := make([]agentcore.ToolCall, 0, len(calls)+1)
	toolsUsed = append(toolsUsed, agentcore.ToolCall{
		Name:    "llm." + a.modelLabel(),
		Success: true,
		Detail:  "eino react orchestration",
	})
	toolsUsed = append(toolsUsed, calls...)

	return &agentcore.Result{
		Intent:          intent,
		Items:           items,
		TotalPriceCents: total,
		Summary:         summaryText,
		ToolsUsed:       toolsUsed,
	}
}

// fallbackSelect 在模型未给出套装时做确定性兜底：必要时先检索候选，再用选择器挑选。
// 检索失败时返回错误详情，由调用方记入工具记录，便于排查"为什么没选出商品"。
func (a *Agent) fallbackSelect(ctx context.Context, input agentcore.Input, intent agentcore.Intent, s *session) ([]agentcore.BundleItem, int64, error) {
	if !s.hasCandidates() {
		products, err := a.provider.SearchProducts(ctx, tools.SearchProductsReq{
			Query:       input.Query,
			Keywords:    intent.Keywords,
			BudgetCents: intent.BudgetCents,
			MaxItems:    intent.MaxItems,
		})
		if err != nil {
			return nil, 0, err
		}
		s.storeCandidates(products)
	}
	items, total := a.selector.Select(s.filterCandidates(nil), intent)
	return items, total, nil
}

// modelLabel 返回模型类型标识，用于工具记录；模型未实现 GetType 时回落到 "model"。
func (a *Agent) modelLabel() string {
	if typed, ok := a.model.(interface{ GetType() string }); ok {
		if name := strings.TrimSpace(typed.GetType()); name != "" {
			return strings.ToLower(name)
		}
	}
	return "model"
}

// deterministicSummary 生成无模型文本时的兜底摘要。
func deterministicSummary(count int, total, budget int64) string {
	if count == 0 {
		return "No bundle was found within the current budget."
	}
	if budget <= 0 {
		return fmt.Sprintf("Selected %d items with total price %.2f.", count, float64(total)/100)
	}
	return fmt.Sprintf("Selected %d items with total price %.2f, within budget %.2f.", count, float64(total)/100, float64(budget)/100)
}
