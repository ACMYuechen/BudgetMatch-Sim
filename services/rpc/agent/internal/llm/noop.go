package llm

import (
	"context"
	"strings"
)

type NoopClient struct{}

func NewNoopClient() *NoopClient {
	return &NoopClient{}
}

func (c *NoopClient) Name() string {
	return "noop"
}

func (c *NoopClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Response{
		FinalText: fallbackText(req),
	}, nil
}

func fallbackText(req Request) string {
	var sections []string
	for _, message := range req.Messages {
		if message.Role == "user" && strings.TrimSpace(message.Content) != "" {
			sections = append(sections, strings.TrimSpace(message.Content))
		}
	}
	if len(sections) == 0 {
		return "Noop LLM client is enabled. No model response was generated."
	}
	return "Noop LLM client is enabled. Model call skipped; use deterministic recommendation result instead."
}
