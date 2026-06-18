package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestNewClientOpenAIRequiresAPIKey(t *testing.T) {
	if _, err := NewClient(Config{Provider: "openai"}); err == nil {
		t.Fatal("expected missing api key error")
	}
}

func TestOpenAIClientParsesToolCalls(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "search_products",
							"arguments": "{\"query\":\"study\"}"
						}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "openai",
		Model:    "test-model",
		BaseURL:  server.URL,
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	resp, err := client.Complete(context.Background(), Request{
		Messages: []prompt.Message{{Role: "user", Content: "recommend"}},
		Tools: []prompt.FunctionTool{{
			Name: "search_products",
			Type: "function",
			Parameters: map[string]any{
				"type": "object",
			},
		}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("unexpected auth header %q", gotAuth)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("unexpected model in request: %+v", gotBody)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "search_products" {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
}
