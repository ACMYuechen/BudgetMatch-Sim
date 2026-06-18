package eino

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/modelconfig"
)

func TestNewChatModelDefaultsToDisabled(t *testing.T) {
	model, err := NewChatModel(modelconfig.Config{})
	if err != nil {
		t.Fatalf("NewChatModel() error = %v", err)
	}
	if model != nil {
		t.Fatalf("expected disabled model, got %T", model)
	}
}

func TestNewChatModelOpenAIRequiresAPIKey(t *testing.T) {
	if _, err := NewChatModel(modelconfig.Config{Provider: "openai"}); err == nil {
		t.Fatal("expected missing api key error")
	}
}

func TestOpenAIChatModelParsesToolCalls(t *testing.T) {
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

	base := NewOpenAIChatModel(modelconfig.Config{
		Provider: "openai",
		Model:    "test-model",
		BaseURL:  server.URL,
		APIKey:   "test-key",
	})
	model, err := base.WithTools([]*schema.ToolInfo{{
		Name: toolkit.ToolSearchProducts,
		Desc: "Search products.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: schema.String, Required: true},
		}),
	}})
	if err != nil {
		t.Fatalf("WithTools() error = %v", err)
	}

	resp, err := model.Generate(context.Background(), []*schema.Message{schema.UserMessage("recommend")}, einomodel.WithTemperature(0.2))
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
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
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != toolkit.ToolSearchProducts {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
}
