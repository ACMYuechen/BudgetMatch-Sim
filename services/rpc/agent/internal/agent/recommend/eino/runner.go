// Package eino 提供基于 Eino 框架的 LLM 模型封装、Prompt 构建、工具定义与 ReAct Agent 运行器。
package eino

import (
	"context"
	"encoding/json"
	"errors"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	einoagent "github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

// defaultMaxStep 是 ReAct Agent 的默认最大步数。
const defaultMaxStep = 8

// Runner 是基于 Eino ReAct 的推荐精修器。
// 它实现 recommend.Refiner 接口，对规则推荐草稿进行 LLM function calling 精修。
type Runner struct {
	model    einomodel.ToolCallingChatModel // model 底层工具调用聊天模型
	provider tools.ProductProvider          // provider 商品数据源
	selector *selector.BundleSelector       // selector 商品组合选择器
	mcpCfg   mcp.Config                     // mcpCfg MCP 配置
	maxStep  int                            // maxStep ReAct 最大步数
}

// ToolResult 记录单次工具调用的结果，包含调用 ID、工具名、JSON 输出及可能的错误信息。
type ToolResult struct {
	CallID string          // CallID 模型生成的工具调用 ID
	Name   string          // Name 工具名称
	JSON   json.RawMessage // JSON 工具返回的 JSON 内容
	Error  string          // Error 工具执行错误信息
}

// 确保 Runner 实现推荐精修接口。
var _ recommendagent.Refiner = (*Runner)(nil)

// NewRunner 创建一个新的 Runner 实例，使用默认最大步数。
func NewRunner(model einomodel.ToolCallingChatModel, provider tools.ProductProvider, selector *selector.BundleSelector, mcpCfg mcp.Config) *Runner {
	return &Runner{
		model:    model,
		provider: provider,
		selector: selector,
		mcpCfg:   mcpCfg,
		maxStep:  defaultMaxStep,
	}
}

// WithMaxStep 设置 ReAct Agent 的最大步数，仅接受大于 0 的值。
func (r *Runner) WithMaxStep(maxStep int) *Runner {
	if maxStep > 0 {
		r.maxStep = maxStep
	}
	return r
}

// Enabled 判断 Runner 是否可用（模型已正确初始化）。
func (r *Runner) Enabled() bool {
	return r != nil && r.model != nil
}

// Name 返回 Runner 的名称标识，若底层模型实现了 Name() 方法则拼接为 "eino.{modelName}"。
func (r *Runner) Name() string {
	if r == nil || r.model == nil {
		return "eino"
	}
	if named, ok := r.model.(interface{ Name() string }); ok {
		return "eino." + named.Name()
	}
	return "eino"
}

// Refine 执行完整的 Eino ReAct 精修流程。
// 返回值是业务态的 RefineResult，调用方无需解析 Eino 消息或工具 JSON。
func (r *Runner) Refine(ctx context.Context, input recommendagent.RefineInput) (*recommendagent.RefineResult, error) {
	if r.model == nil {
		return nil, errors.New("eino chat model is nil")
	}
	if r.provider == nil {
		return nil, errors.New("product provider is nil")
	}
	if r.selector == nil {
		return nil, errors.New("bundle selector is nil")
	}

	agent, future, option, err := r.newAgent(ctx)
	if err != nil {
		return nil, err
	}

	final, err := agent.Generate(ctx, BuildMessages(input), option)
	if err != nil {
		return nil, err
	}
	if final == nil {
		return nil, errors.New("eino agent returned nil message")
	}

	return refineResultFromTools(final.Content, collectToolResults(future)), nil
}

// newAgent 创建 ReAct Agent 实例，将 toolkit 中的工具注册到 Eino 工具节点中，
// 并返回用于收集工具调用历史的 MessageFuture。
func (r *Runner) newAgent(ctx context.Context) (*react.Agent, react.MessageFuture, einoagent.AgentOption, error) {
	executor := toolkit.NewExecutor(r.provider, r.selector).WithMCP(r.mcpCfg)
	einoTools := NewTools(executor)

	option, future := react.WithMessageFuture()
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: r.model,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: einoTools,
		},
		MaxStep: r.maxStep,
	})
	if err != nil {
		return nil, nil, einoagent.AgentOption{}, err
	}

	return agent, future, option, nil
}

// collectToolResults 从 MessageFuture 中遍历所有消息，提取工具调用结果，
// 将 Assistant 消息中的 ToolCall 与 Tool 消息中的结果一一对应。
func collectToolResults(future react.MessageFuture) []ToolResult {
	iter := future.GetMessages()
	if iter == nil {
		return nil
	}

	callNames := make(map[string]string)
	var results []ToolResult
	for {
		msg, ok, err := iter.Next()
		if err != nil {
			results = append(results, ToolResult{Error: err.Error()})
			continue
		}
		if !ok {
			break
		}
		if msg == nil {
			continue
		}
		if msg.Role == schema.Assistant {
			for _, call := range msg.ToolCalls {
				if call.ID != "" {
					callNames[call.ID] = call.Function.Name
				}
			}
			continue
		}
		if msg.Role != schema.Tool {
			continue
		}

		name := msg.ToolName
		if name == "" {
			name = callNames[msg.ToolCallID]
		}
		results = append(results, toolResultFromMessage(name, msg))
	}
	return results
}

// toolResultFromMessage 将单条 Eino Tool 消息转换为 ToolResult，
// 并尝试解析消息内容中的 success/error 字段以提取错误信息。
func toolResultFromMessage(name string, msg *schema.Message) ToolResult {
	result := ToolResult{
		CallID: msg.ToolCallID,
		Name:   name,
		JSON:   json.RawMessage(msg.Content),
	}

	var errorPayload struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &errorPayload); err == nil &&
		errorPayload.Success != nil && !*errorPayload.Success && errorPayload.Error != "" {
		result.Error = errorPayload.Error
	}
	return result
}

// refineResultFromTools 将 Eino 工具调用结果转换为推荐业务精修结果。
func refineResultFromTools(finalText string, tools []ToolResult) *recommendagent.RefineResult {
	result := &recommendagent.RefineResult{
		Summary: finalText,
	}
	for _, tool := range tools {
		result.ToolsUsed = append(result.ToolsUsed, toolCallFromResult(tool))
		if tool.Name == toolkit.ToolSelectBundle && tool.Error == "" {
			applySelectedBundle(result, tool.JSON)
		}
	}
	return result
}

// toolCallFromResult 将 Eino 工具结果转换为通用工具调用记录。
func toolCallFromResult(tool ToolResult) agentcore.ToolCall {
	return agentcore.ToolCall{
		Name:    "tool." + tool.Name,
		Success: tool.Error == "",
		Detail:  toolDetail(tool),
	}
}

// toolDetail 提取工具调用详情，优先返回错误信息，其次返回原始 JSON 结果。
func toolDetail(tool ToolResult) string {
	if tool.Error != "" {
		return tool.Error
	}
	if len(tool.JSON) == 0 {
		return "completed"
	}
	return string(tool.JSON)
}

// applySelectedBundle 从 select_bundle 工具结果中回填最终商品组合。
func applySelectedBundle(result *recommendagent.RefineResult, raw json.RawMessage) {
	var selected toolkit.SelectBundleResult
	if err := json.Unmarshal(raw, &selected); err != nil || len(selected.Items) == 0 {
		return
	}
	result.Items = selected.Items
	result.TotalPriceCents = selected.TotalPriceCents
}
