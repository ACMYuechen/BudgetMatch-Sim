package eino

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/modelconfig"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com"
	defaultOpenAIModel   = "gpt-4.1-mini"
)

type OpenAIChatModel struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	tools      []*schema.ToolInfo
}

var _ einomodel.ToolCallingChatModel = (*OpenAIChatModel)(nil)

func NewChatModel(c modelconfig.Config) (einomodel.ToolCallingChatModel, error) {
	switch c.ProviderName() {
	case "noop":
		return nil, nil
	case "openai":
		if strings.TrimSpace(c.APIKey) == "" {
			return nil, fmt.Errorf("openai api key is required when model provider is openai")
		}
		return NewOpenAIChatModel(c), nil
	default:
		return nil, fmt.Errorf("unsupported model provider %q", c.Provider)
	}
}

func NewOpenAIChatModel(c modelconfig.Config) *OpenAIChatModel {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = defaultOpenAIModel
	}

	return &OpenAIChatModel{
		baseURL: baseURL,
		apiKey:  c.APIKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (m *OpenAIChatModel) Name() string {
	return "openai"
}

func (m *OpenAIChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	clone := *m
	clone.tools = append([]*schema.ToolInfo(nil), tools...)
	return &clone, nil
}

func (m *OpenAIChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	options := einomodel.GetCommonOptions(&einomodel.Options{
		Model: &m.model,
		Tools: append([]*schema.ToolInfo(nil), m.tools...),
	}, opts...)

	modelName := m.model
	if options.Model != nil && strings.TrimSpace(*options.Model) != "" {
		modelName = strings.TrimSpace(*options.Model)
	}

	tools, err := toOpenAITools(options.Tools)
	if err != nil {
		return nil, err
	}

	payload := chatCompletionRequest{
		Model:       modelName,
		Messages:    toOpenAIMessages(input),
		Tools:       tools,
		Temperature: options.Temperature,
		TopP:        options.TopP,
		MaxTokens:   options.MaxTokens,
		Stop:        options.Stop,
	}
	if len(payload.Tools) > 0 {
		payload.ToolChoice = openAIToolChoice(options.ToolChoice)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode openai chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create openai chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call openai chat completions: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai chat response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai chat completions failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded chatCompletionResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode openai chat response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("openai chat response has no choices")
	}

	message := decoded.Choices[0].Message
	return schema.AssistantMessage(message.Content, toEinoToolCalls(message.ToolCalls)), nil
}

func (m *OpenAIChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type chatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
	Temperature *float32        `json:"temperature,omitempty"`
	TopP        *float32        `json:"top_p,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

func toOpenAIMessages(messages []*schema.Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		out = append(out, openAIMessage{
			Role:       string(message.Role),
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  toOpenAIToolCalls(message.ToolCalls),
		})
	}
	return out
}

func toOpenAITools(tools []*schema.ToolInfo) ([]openAITool, error) {
	out := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || tool.Name == "" {
			continue
		}
		var params any
		if tool.ParamsOneOf != nil {
			jsonSchema, err := tool.ParamsOneOf.ToJSONSchema()
			if err != nil {
				return nil, fmt.Errorf("convert tool %s params: %w", tool.Name, err)
			}
			params = jsonSchema
		}
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        tool.Name,
				Description: tool.Desc,
				Parameters:  params,
			},
		})
	}
	return out, nil
}

func toOpenAIToolCalls(calls []schema.ToolCall) []openAIToolCall {
	out := make([]openAIToolCall, 0, len(calls))
	for _, call := range calls {
		callType := call.Type
		if callType == "" {
			callType = "function"
		}
		out = append(out, openAIToolCall{
			ID:   call.ID,
			Type: callType,
			Function: openAIToolCallFunction{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

func toEinoToolCalls(calls []openAIToolCall) []schema.ToolCall {
	out := make([]schema.ToolCall, 0, len(calls))
	for _, call := range calls {
		if call.Function.Name == "" {
			continue
		}
		callType := call.Type
		if callType == "" {
			callType = "function"
		}
		out = append(out, schema.ToolCall{
			ID:   call.ID,
			Type: callType,
			Function: schema.FunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

func openAIToolChoice(choice *schema.ToolChoice) any {
	if choice == nil {
		return "auto"
	}

	switch *choice {
	case schema.ToolChoiceForbidden:
		return "none"
	case schema.ToolChoiceForced:
		return "required"
	default:
		return "auto"
	}
}
