package llm

import (
	"context"
	"encoding/json"

	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/prompt"
)

type Client interface {
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
}

type Request struct {
	Messages []prompt.Message      `json:"messages"`
	Tools    []prompt.FunctionTool `json:"tools,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Response struct {
	FinalText string     `json:"final_text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

func (r Response) NeedsToolCall() bool {
	return len(r.ToolCalls) > 0
}
