package llm

import (
	"context"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/prompt"
)

func TestNewClientDefaultsToNoop(t *testing.T) {
	client, err := NewClient(Config{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.Name() != "noop" {
		t.Fatalf("expected noop client, got %q", client.Name())
	}
}

func TestNoopClientComplete(t *testing.T) {
	client := NewNoopClient()

	resp, err := client.Complete(context.Background(), Request{
		Messages: []prompt.Message{
			{Role: "system", Content: "system"},
			{Role: "user", Content: "recommend a bundle"},
		},
		Tools: []prompt.FunctionTool{
			{Name: "search_products", Type: "function"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.FinalText == "" {
		t.Fatal("expected fallback final text")
	}
	if resp.NeedsToolCall() {
		t.Fatalf("noop client should not request tools, got %+v", resp.ToolCalls)
	}
}

func TestNewClientRejectsUnsupportedProvider(t *testing.T) {
	if _, err := NewClient(Config{Provider: "unknown"}); err == nil {
		t.Fatal("expected unsupported provider error before openai client is implemented")
	}
}

func TestNewClientOpenAIWithoutAPIKeyFallsBackToNoop(t *testing.T) {
	client, err := NewClient(Config{Provider: "openai"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.Name() != "noop" {
		t.Fatalf("expected noop fallback, got %q", client.Name())
	}
}

func TestNewClientOpenAIWithAPIKeyIsNotImplemented(t *testing.T) {
	if _, err := NewClient(Config{Provider: "openai", APIKey: "test-key"}); err == nil {
		t.Fatal("expected not implemented error")
	}
}
