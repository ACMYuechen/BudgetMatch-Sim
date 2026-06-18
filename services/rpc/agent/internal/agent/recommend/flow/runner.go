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
	model    llm.Client
	executor *toolkit.Executor
	maxRound int
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
	return &Runner{
		model:    model,
		executor: executor,
		maxRound: defaultMaxRounds,
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
	if r.executor == nil {
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

		for _, call := range resp.ToolCalls {
			toolResult := r.executeTool(ctx, call)
			toolResults = append(toolResults, toolResult)
			messages = append(messages, prompt.Message{
				Role:    "tool",
				Content: toolMessageContent(toolResult),
			})
		}
	}

	return nil, fmt.Errorf("function calling exceeded max rounds: %d", r.maxRound)
}

func (r *Runner) executeTool(ctx context.Context, call llm.ToolCall) ToolResult {
	result, err := r.executor.Execute(ctx, call.Name, call.Arguments)
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
