// Package eino 提供基于 Eino 框架的 LLM 模型封装、Prompt 构建、工具定义与 ReAct Agent 运行器。
// 该包将 OpenAI 兼容的 Chat Completion API 接入 Eino 的 ToolCallingChatModel 接口，
// 并组装 search_products、select_bundle、mcp_call_tool 等工具，实现商品推荐的 Agent 推理链路。
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
	// defaultOpenAIBaseURL 是 OpenAI API 的默认基础地址。
	defaultOpenAIBaseURL = "https://api.openai.com"
	// defaultOpenAIModel 是默认使用的 OpenAI 模型名称。
	defaultOpenAIModel = "gpt-4.1-mini"
)

// OpenAIChatModel 是一个基于 OpenAI Chat Completion API 的聊天模型实现，
// 支持工具调用（Tool Calling），可作为 Eino 的 ToolCallingChatModel 使用。
type OpenAIChatModel struct {
	baseURL    string             // 基础请求地址
	apiKey     string             // API 密钥
	model      string             // 模型名称
	httpClient *http.Client       // HTTP 客户端
	tools      []*schema.ToolInfo // 绑定的工具信息列表
}

// 确保 OpenAIChatModel 实现了 einomodel.ToolCallingChatModel 接口。
var _ einomodel.ToolCallingChatModel = (*OpenAIChatModel)(nil)

// NewChatModel 根据 modelconfig.Config 创建对应的聊天模型。
// 当 Provider 为 "noop" 时返回 nil；当 Provider 为 "openai" 时创建 OpenAIChatModel。
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

// NewOpenAIChatModel 使用配置创建一个新的 OpenAIChatModel 实例。
// 若 BaseURL 或 Model 为空，则使用默认值；HTTP 超时固定为 60 秒。
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

// Name 返回模型名称标识。
func (m *OpenAIChatModel) Name() string {
	return "openai"
}

// WithTools 为当前模型绑定工具信息，返回一个携带工具的新模型实例（原实例不变）。
func (m *OpenAIChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	clone := *m
	clone.tools = append([]*schema.ToolInfo(nil), tools...)
	return &clone, nil
}

// Generate 向 OpenAI Chat Completion API 发起同步请求，返回模型生成的消息。
// 支持通过 opts 覆盖模型名称、温度、工具选择等参数。
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

// Stream 将 Generate 的结果包装为流式读取器（当前实现为伪流式：一次性返回全部结果）。
func (m *OpenAIChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// chatCompletionRequest 表示 OpenAI Chat Completion API 的请求体结构。
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

// openAIMessage 表示 OpenAI 消息格式中的单条消息。
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

// openAITool 表示 OpenAI 工具定义中的工具结构。
type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

// openAIFunction 表示 OpenAI 工具中的函数定义。
type openAIFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// openAIToolCall 表示 OpenAI 响应中的单次工具调用。
type openAIToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openAIToolCallFunction `json:"function"`
}

// openAIToolCallFunction 表示工具调用中的函数信息。
type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatCompletionResponse 表示 OpenAI Chat Completion API 的响应体结构。
type chatCompletionResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

// toOpenAIMessages 将 Eino 的 schema.Message 切片转换为 OpenAI 消息格式。
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

// toOpenAITools 将 Eino 的 schema.ToolInfo 切片转换为 OpenAI 工具定义格式。
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

// toOpenAIToolCalls 将 Eino 的 schema.ToolCall 切片转换为 OpenAI 工具调用格式。
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

// toEinoToolCalls 将 OpenAI 工具调用格式转换回 Eino 的 schema.ToolCall 切片。
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

// openAIToolChoice 将 Eino 的 ToolChoice 枚举转换为 OpenAI 的 tool_choice 字符串值。
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
