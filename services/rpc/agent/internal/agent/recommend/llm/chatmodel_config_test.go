package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	modelconfig "budgetmatch-sim/services/rpc/agent/internal/model"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

// Parse configuration and exercise the pinned SDK without the acceptance proxy:
// the provider-specific option must survive Generate, Stream and WithTools.
func TestChatModelThinkingWireContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		model    string
		thinking string
		stream   bool
		bound    bool
	}{
		{name: "flash_generate", model: "deepseek-flash", thinking: "disabled"},
		{name: "flash_generate_bound", model: "deepseek-flash", thinking: "disabled", bound: true},
		{name: "flash_stream", model: "deepseek-flash", thinking: "disabled", stream: true},
		{name: "flash_stream_bound", model: "deepseek-flash", thinking: "disabled", stream: true, bound: true},
		{name: "generic_generate", model: "local-fixture"},
		{name: "generic_stream_bound", model: "local-fixture", stream: true, bound: true},
		{name: "legacy_default_model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type wireRequest struct {
				Model         string            `json:"model"`
				Thinking      json.RawMessage   `json:"thinking"`
				Stream        bool              `json:"stream"`
				StreamOptions map[string]bool   `json:"stream_options"`
				Tools         []json.RawMessage `json:"tools"`
				MaxTokens     int               `json:"max_tokens"`
			}
			requests := make(chan wireRequest, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" ||
					r.Header.Get("Authorization") != "Bearer local-fixture-key" {
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				var request wireRequest
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&request); err != nil {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				select {
				case requests <- request:
				default:
					http.Error(w, "unexpected retry", http.StatusBadRequest)
					return
				}
				if request.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"fixture\"}}]}\n\n")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\n")
					fmt.Fprint(w, "data: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"fixture"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
			}))
			defer server.Close()
			var cfg modelconfig.Config
			require.NoError(t, conf.LoadFromYamlBytes([]byte(fmt.Sprintf(
				"Provider: openai\nModel: %q\nThinking: %q\nBaseURL: %q\nAPIKey: local-fixture-key\n",
				tc.model, tc.thinking, server.URL+"/v1")), &cfg))
			ctx := t.Context()
			chat, err := NewChatModel(ctx, cfg)
			require.NoError(t, err)
			if tc.bound {
				chat, err = chat.WithTools([]*schema.ToolInfo{{Name: "select_bundle", Desc: "synthetic only"}})
				require.NoError(t, err)
			}
			messages := []*schema.Message{{Role: schema.User, Content: "synthetic only"}}
			if tc.stream {
				stream, err := chat.Stream(ctx, messages, model.WithMaxTokens(512))
				require.NoError(t, err)
				defer stream.Close()
				var usage *schema.TokenUsage
				for {
					message, err := stream.Recv()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					if message.ResponseMeta != nil && message.ResponseMeta.Usage != nil {
						usage = message.ResponseMeta.Usage
					}
				}
				require.NotNil(t, usage)
				require.Equal(t, 10, usage.TotalTokens)
			} else {
				message, err := chat.Generate(ctx, messages, model.WithMaxTokens(512))
				require.NoError(t, err)
				require.Equal(t, "fixture", message.Content)
			}
			require.Len(t, requests, 1)
			request := <-requests
			expectedModel := tc.model
			if expectedModel == "" {
				expectedModel = defaultModel
			}
			require.Equal(t, expectedModel, request.Model)
			require.Equal(t, tc.stream, request.Stream)
			require.Equal(t, 512, request.MaxTokens)
			if tc.thinking != "" {
				require.JSONEq(t, `{"type":"disabled"}`, string(request.Thinking))
			} else {
				require.Empty(t, request.Thinking, "legacy models must not receive provider-specific fields")
			}
			if tc.bound {
				require.Len(t, request.Tools, 1)
			} else {
				require.Empty(t, request.Tools)
			}
			if tc.stream {
				require.True(t, request.StreamOptions["include_usage"])
			} else {
				require.Empty(t, request.StreamOptions)
			}
		})
	}
}

func TestNoopChatModelDoesNotInitializeSDK(t *testing.T) {
	chat, err := NewChatModel(context.Background(), modelconfig.Config{Model: "deepseek-flash"})
	require.NoError(t, err)
	require.Nil(t, chat)
}

func TestChatModelFactoryRejectsInvalidConfiguration(t *testing.T) {
	for _, cfg := range []modelconfig.Config{
		{Provider: "openai", Model: "deepseek-flash", APIKey: "fixture-key"},
		{Provider: "openai", Model: "deepseek-flash", Thinking: "enabled", APIKey: "fixture-key"},
		{Provider: "openai", Model: "local-fixture", Thinking: "disabled", APIKey: "fixture-key"},
		{Provider: "openai"},
		{Provider: "unsupported-provider"},
	} {
		t.Run(cfg.Provider+"/"+cfg.Model+"/"+cfg.Thinking, func(t *testing.T) {
			// No HTTP request should be possible even outside ServiceContext.
			cfg.BaseURL = "http://127.0.0.1:1/v1"
			expected := cfg.Validate()
			require.Error(t, expected)
			chat, err := NewChatModel(t.Context(), cfg)
			require.EqualError(t, err, expected.Error())
			require.Nil(t, chat)
		})
	}
}
