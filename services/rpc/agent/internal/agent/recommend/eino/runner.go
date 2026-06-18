package eino

import (
	"context"
	"encoding/json"
	"errors"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
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

const defaultMaxStep = 8

type Runner struct {
	model    einomodel.ToolCallingChatModel
	provider tools.ProductProvider
	selector *selector.BundleSelector
	mcpCfg   mcp.Config
	maxStep  int
}

type RunInput struct {
	Query           string
	Intent          agentcore.Intent
	SelectedItems   []agentcore.BundleItem
	TotalPriceCents int64
	ToolsUsed       []agentcore.ToolCall
}

type RunResult struct {
	FinalText   string
	ToolResults []ToolResult
}

type ToolResult struct {
	CallID string
	Name   string
	JSON   json.RawMessage
	Error  string
}

func NewRunner(model einomodel.ToolCallingChatModel, provider tools.ProductProvider, selector *selector.BundleSelector, mcpCfg mcp.Config) *Runner {
	return &Runner{
		model:    model,
		provider: provider,
		selector: selector,
		mcpCfg:   mcpCfg,
		maxStep:  defaultMaxStep,
	}
}

func (r *Runner) WithMaxStep(maxStep int) *Runner {
	if maxStep > 0 {
		r.maxStep = maxStep
	}
	return r
}

func (r *Runner) Enabled() bool {
	return r != nil && r.model != nil
}

func (r *Runner) Name() string {
	if r == nil || r.model == nil {
		return "eino"
	}
	if named, ok := r.model.(interface{ Name() string }); ok {
		return "eino." + named.Name()
	}
	return "eino"
}

func (r *Runner) Run(ctx context.Context, input RunInput) (*RunResult, error) {
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

	return &RunResult{
		FinalText:   final.Content,
		ToolResults: collectToolResults(future),
	}, nil
}

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
