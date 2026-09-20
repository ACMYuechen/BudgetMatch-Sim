package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	modelconfig "budgetmatch-sim/services/rpc/agent/internal/model"
	"budgetmatch-sim/services/rpc/agent/internal/runtrace"
	"budgetmatch-sim/services/rpc/agent/streamcontract"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// Exercise the pinned SDK, not a permissive ToolCallingChatModel stub. No .env,
// provider credentials or external HTTP endpoints are used by this regression.
func TestExplanationStreamUsesSDKPerCallEmptyTools(t *testing.T) {
	for _, tc := range []struct {
		model, thinking string
		bound           bool
	}{
		{model: "local-fixture"},
		{model: "local-fixture", bound: true},
		{model: "deepseek-flash", thinking: "disabled"},
		{model: "deepseek-flash", thinking: "disabled", bound: true},
	} {
		t.Run(fmt.Sprintf("model=%s/bound=%v", tc.model, tc.bound), func(t *testing.T) {
			type wireRequest struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
				Tools      []json.RawMessage `json:"tools"`
				ToolChoice string            `json:"tool_choice"`
				MaxTokens  int               `json:"max_tokens"`
				Stream     bool              `json:"stream"`
				Thinking   json.RawMessage   `json:"thinking"`
			}
			requests := make(chan wireRequest, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
					http.Error(w, "unexpected endpoint", http.StatusBadRequest)
					return
				}
				var request wireRequest
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&request); err != nil {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				requests <- request
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"预算内的合成说明\"}}]}\n\n")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":10,\"total_tokens\":110}}\n\n")
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			base, err := NewChatModel(context.Background(), modelconfig.Config{
				Provider: "openai", Model: tc.model, Thinking: tc.thinking,
				BaseURL: server.URL + "/v1", APIKey: "local-fixture-key",
			})
			require.NoError(t, err)
			runner := newBoundedStreamModel(base, 1000)
			if tc.bound {
				withTools, err := runner.WithTools([]*schema.ToolInfo{{Name: "private_tool", Desc: strings.Repeat("private description", 1000)}})
				require.NoError(t, err)
				runner = withTools.(*boundedStreamModel)
			}
			ctx, recorder := runtrace.Start(context.Background(), "sdk-explanation")
			progress := &toolProgressProbe{}
			result := &agentcore.Result{
				Intent: agentcore.Intent{BudgetCents: 60000, MaxItems: 3, Keywords: []string{"PRIVATE query"}},
				Items:  []agentcore.BundleItem{{Id: "PRIVATE id", Name: "PRIVATE item"}}, TotalPriceCents: 12300,
			}
			require.NoError(t, streamExplanation(ctx, runner, result, progress))
			require.Len(t, progress.events, 1)
			require.Equal(t, streamcontract.AnswerDelta, progress.events[0].Kind)
			require.Equal(t, "预算内的合成说明", progress.events[0].Text)
			require.Len(t, requests, 1)
			request := <-requests
			require.True(t, request.Stream)
			if tc.thinking != "" {
				require.JSONEq(t, `{"type":"disabled"}`, string(request.Thinking))
			} else {
				require.Empty(t, request.Thinking)
			}
			require.Empty(t, request.Tools, "pre-bound schemas must not cross the numeric explanation boundary")
			require.Equal(t, "none", request.ToolChoice)
			require.Equal(t, 512, request.MaxTokens)
			require.Len(t, request.Messages, 2)
			require.Equal(t, "system", request.Messages[0].Role)
			require.Equal(t, "user", request.Messages[1].Role)
			require.JSONEq(t, `{"budget_cents":60000,"max_items":3,"item_count":1,"total_price_cents":12300}`, request.Messages[1].Content)
			encoded, err := json.Marshal(request)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "PRIVATE")
			require.NotContains(t, string(encoded), "private_tool")
			summary := recorder.Finish(nil, false)
			require.Equal(t, 1, summary.ModelCalls)
			require.Equal(t, 1, summary.UsageKnownCalls)
			require.Equal(t, &runtrace.Usage{Prompt: 100, Completion: 10, Total: 110}, summary.ReportedUsage)
		})
	}
}
