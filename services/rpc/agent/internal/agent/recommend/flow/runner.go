package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/prompt"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/llm"
)

const defaultMaxRounds = 3

type Runner struct {
	model       llm.Client
	newExecutor func() *toolkit.Executor
	maxRound    int
}

type RunInput struct {
	Messages []prompt.Message
	Tools    []prompt.FunctionTool
}

type RunResult struct {
	FinalText   string
	Messages    []prompt.Message
	ToolResults []ToolResult
	Rounds      int
}

type ToolResult struct {
	CallID string
	Name   string
	JSON   json.RawMessage
	Error  string
}

func NewRunner(model llm.Client, executor *toolkit.Executor) *Runner {
	return NewRunnerWithFactory(model, func() *toolkit.Executor {
		return executor
	})
}

func NewRunnerWithFactory(model llm.Client, newExecutor func() *toolkit.Executor) *Runner {
	return &Runner{
		model:       model,
		newExecutor: newExecutor,
		maxRound:    defaultMaxRounds,
	}
}

func (r *Runner) WithMaxRounds(maxRound int) *Runner {
	if maxRound > 0 {
		r.maxRound = maxRound
	}
	return r
}

func (r *Runner) Run(ctx context.Context, input RunInput) (*RunResult, error) {
	if r.model == nil {
		return nil, errors.New("llm client is nil")
	}
	if r.newExecutor == nil {
		return nil, errors.New("tool executor factory is nil")
	}
	executor := r.newExecutor()
	if executor == nil {
		return nil, errors.New("tool executor is nil")
	}

	messages := append([]prompt.Message(nil), input.Messages...)
	var toolResults []ToolResult

	for round := 1; round <= r.maxRound; round++ {
		resp, err := r.model.Complete(ctx, llm.Request{
			Messages: messages,
			Tools:    input.Tools,
		})
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, errors.New("llm response is nil")
		}
		if !resp.NeedsToolCall() {
			return &RunResult{
				FinalText:   resp.FinalText,
				Messages:    messages,
				ToolResults: toolResults,
				Rounds:      round,
			}, nil
		}

		messages = append(messages, assistantToolCallMessage(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			toolResult := executeTool(ctx, executor, call)
			toolResults = append(toolResults, toolResult)
			messages = append(messages, prompt.Message{
				Role:       "tool",
				ToolCallID: toolResult.CallID,
				Content:    toolMessageContent(toolResult),
			})
		}
	}

	return nil, fmt.Errorf("function calling exceeded max rounds: %d", r.maxRound)
}

func executeTool(ctx context.Context, executor *toolkit.Executor, call llm.ToolCall) ToolResult {
	result, err := executor.Execute(ctx, call.Name, call.Arguments)
	if err != nil {
		return ToolResult{
			CallID: call.ID,
			Name:   call.Name,
			Error:  err.Error(),
		}
	}
	return ToolResult{
		CallID: call.ID,
		Name:   call.Name,
		JSON:   result.JSON,
	}
}

func assistantToolCallMessage(calls []llm.ToolCall) prompt.Message {
	message := prompt.Message{
		Role:      "assistant",
		ToolCalls: make([]prompt.ToolCall, 0, len(calls)),
	}
	for _, call := range calls {
		message.ToolCalls = append(message.ToolCalls, prompt.ToolCall{
			ID:   call.ID,
			Type: "function",
			Function: prompt.ToolCallFunction{
				Name:      call.Name,
				Arguments: string(call.Arguments),
			},
		})
	}
	return message
}

func toolMessageContent(result ToolResult) string {
	payload := map[string]any{
		"tool_call_id": result.CallID,
		"name":         result.Name,
	}
	if result.Error != "" {
		payload["success"] = false
		payload["error"] = result.Error
	} else {
		payload["success"] = true
		payload["result"] = json.RawMessage(result.JSON)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"success":false,"name":%q,"error":%q}`, result.Name, err.Error())
	}
	return string(data)
}
