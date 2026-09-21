package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/einolog"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/runtrace"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	einoagent "github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// logCallbacks 是全局复用的 Eino 组件日志回调：
// 挂在 ReAct Generate/Stream 上；流式 Usage 另由 boundedStreamModel 按请求汇总，
// 不依赖可能缺失的同步 OnEnd，也不把 Token 数当成费用。
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
	fileCfg          filetools.Config
	maxStep          int
	memory           memory.Manager // memory 会话记忆，只读取历史；写入统一由 Service 层完成
	maxHistory       int            // maxHistory 单次读取的最大历史条数
	maxContextTokens int            // maxContextTokens 发送给模型的消息上下文近似 token 上限
}

// 确保 Agent 实现 agentcore.Agent。
var _ agentcore.Agent = (*Agent)(nil)
var _ agentcore.StreamingAgent = (*Agent)(nil)

// NewAgent 创建基于 Eino ReAct 的推荐 Agent。
func NewAgent(m model.ToolCallingChatModel, provider tools.ProductProvider, sel *selector.BundleSelector,
	mcpCfg mcpconfig.Config, fileCfg filetools.Config) *Agent {
	mcpCfg.Args = append([]string(nil), mcpCfg.Args...)
	mcpCfg.AllowedTools = append([]string(nil), mcpCfg.AllowedTools...)
	return &Agent{
		model:            m,
		planner:          recommendagent.NewPlanner(),
		provider:         provider,
		selector:         sel,
		mcpCfg:           mcpCfg,
		fileCfg:          fileCfg.Normalize(),
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
func (a *Agent) Run(ctx context.Context, input agentcore.Input) (result *agentcore.Result, err error) {
	return a.run(ctx, input, nil)
}

// RunStream keeps orchestration private and streams a separate, tool-free
// explanation from a numeric public projection. No history/tool bodies enter
// that explanation call; all text is provisional and never replaces facts.
func (a *Agent) RunStream(ctx context.Context, input agentcore.Input, progress agentcore.ProgressSink) (*agentcore.Result, error) {
	if progress == nil {
		return nil, agentcore.ErrInvalidInput
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result, err := a.run(ctx, input, progress)
	if err != nil {
		return nil, safety.Protect(errors.Join(agentcore.ErrStreamInterrupted, err))
	}
	return result, nil
}

func (a *Agent) run(ctx context.Context, input agentcore.Input, progress agentcore.ProgressSink) (result *agentcore.Result, err error) {
	defer func() { err = safety.Protect(err) }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || a.model == nil {
		return nil, errors.New("llm chat model is not configured")
	}
	if a.provider == nil || a.selector == nil {
		return nil, errors.New("product provider and bundle selector are required")
	}
	runtrace.From(ctx).Provider(a.provider.Name())

	intent, err := a.planner.Resolve(input, nil)
	if err != nil {
		return nil, err
	}
	// 在任何文件/MCP 副作用前校验上下文大小。
	history := a.loadHistory(ctx, input)
	messages, err := buildMessages(input, intent, history, a.maxContextTokens)
	if err != nil {
		return nil, err
	}
	_, writePath, err := filetools.ParseSaveRequest(input.Query)
	if err != nil {
		return nil, agentcore.ErrInvalidInput
	}
	if writePath != "" && !a.fileCfg.Enabled {
		return nil, status.Error(codes.PermissionDenied, "file tools are disabled")
	}
	workspace, err := filetools.NewWorkspace(a.fileCfg, input.UserId, writePath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, status.Error(codes.PermissionDenied, "file tool access denied")
		}
		return nil, err
	}
	if workspace != nil {
		defer workspace.Close()
	}
	s := newSession(a.provider, a.selector, intent)
	s.progress = progress

	reactTools, err := businessTools(s, workspace)
	if err != nil {
		return nil, err
	}
	mcpToolList, cleanup, err := mcpTools(ctx, a.mcpCfg, s)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	reactTools = append(reactTools, mcpToolList...)

	reactConfig := &react.AgentConfig{
		ToolCallingModel: a.model,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: reactTools},
		MaxStep:          a.maxStep,
	}
	var streamingModel *boundedStreamModel
	if progress != nil {
		streamingModel = newBoundedStreamModel(a.model, a.maxContextTokens)
		reactConfig.ToolCallingModel = streamingModel
		reactConfig.MaxStep = min(a.maxStep, defaultMaxStep)
		reactConfig.ToolsConfig.ExecuteSequentially = true
		// Full bounded inspection handles text-before-tool models. These
		// orchestration chunks are NEVER the public answer stream.
		reactConfig.StreamToolCallChecker = inspectToolStream
	}
	reactAgent, err := react.NewAgent(ctx, reactConfig)
	if err != nil {
		return nil, fmt.Errorf("build react agent: %w", err)
	}

	keptHistory := max(len(messages)-2, 0)
	if keptHistory < len(history) {
		logx.WithContext(ctx).Infow("conversation history trimmed by context token budget",
			logx.Field("conversation_id", safety.Label(input.ConversationId)),
			logx.Field("loaded_messages", len(history)),
			logx.Field("kept_messages", keptHistory),
			logx.Field("max_context_tokens", a.maxContextTokens),
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if progress == nil {
		_, err = reactAgent.Generate(ctx, messages,
			einoagent.WithComposeOptions(compose.WithCallbacks(logCallbacks)))
	} else {
		err = drainOrchestration(ctx, reactAgent, messages)
	}
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err = a.assemble(ctx, input, intent, s)
	if err != nil || progress == nil {
		return result, err
	}
	if err := streamExplanation(ctx, streamingModel, result, progress); err != nil {
		return nil, err
	}
	return result, nil
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
			logx.Field("conversation_id", safety.Label(input.ConversationId)),
			logx.Field("error_code", safety.ErrorCode(err)),
		)
		return nil
	}
	return history
}

// assemble 把 session 中累积的类型化结果组装为业务响应。
func (a *Agent) assemble(ctx context.Context, input agentcore.Input, intent agentcore.Intent, s *session) (*agentcore.Result, error) {
	items, total, calls := s.snapshot()
	if !s.hasSelection() {
		var items2Err error
		items, total, items2Err = a.fallbackSelect(ctx, input, intent, s)
		detail := "model produced no bundle; used deterministic selection"
		if items2Err != nil {
			return nil, items2Err
		}
		_, _, calls = s.snapshot() // include fallback retrieval diagnostics
		calls = append(calls, agentcore.ToolCall{
			Name:    "selector.fallback",
			Success: len(items) > 0,
			Detail:  detail,
		})
	}

	toolsUsed := make([]agentcore.ToolCall, 0, len(calls)+1)
	toolsUsed = append(toolsUsed, agentcore.ToolCall{
		Name:    "llm." + a.modelLabel(),
		Success: true,
		Detail:  "eino react orchestration",
	})
	toolsUsed = append(toolsUsed, calls...)

	result := &agentcore.Result{
		Candidates:      s.filterCandidates(nil),
		Selection:       s.selectionScope(),
		Intent:          intent,
		Items:           items,
		TotalPriceCents: total,
		Summary:         agentcore.BundleSummary(len(items), total, intent.BudgetCents),
		ToolsUsed:       toolsUsed,
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, _ := agentcore.NewConstraints(intent)
	if err := limits.ValidateResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

// fallbackSelect 在模型未给出套装时做确定性兜底：必要时先检索候选，再用选择器挑选。
// 检索失败原样返回错误，不能把失败包装成一次成功的空推荐。
func (a *Agent) fallbackSelect(ctx context.Context, input agentcore.Input, intent agentcore.Intent, s *session) ([]agentcore.BundleItem, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if !s.hasCandidates() {
		search, err := tools.SearchWithTrace(ctx, a.provider, tools.SearchProductsReq{
			Query:       input.Query,
			Keywords:    intent.Keywords,
			BudgetCents: intent.BudgetCents,
			MaxItems:    intent.MaxItems,
		})
		for _, call := range safety.ToolCalls(search.Calls) {
			s.recordCall(call)
		}
		if err != nil {
			return nil, 0, err
		}
		s.storeCandidates(search.Candidates)
	}
	items, total := a.selector.Select(s.filterCandidates(nil), intent)
	return items, total, nil
}

// modelLabel 返回模型类型标识，用于工具记录；模型未实现 GetType 时回落到 "model"。
func (a *Agent) modelLabel() string {
	if typed, ok := a.model.(interface{ GetType() string }); ok {
		if name := strings.TrimSpace(typed.GetType()); name != "" {
			return safety.Label(strings.ToLower(name))
		}
	}
	return "model"
}
