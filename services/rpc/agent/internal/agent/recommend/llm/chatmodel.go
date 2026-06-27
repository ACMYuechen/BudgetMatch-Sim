// Package llm 基于 Eino ReAct 实现推荐 Agent 的 LLM 编排能力。
// 模型接入、工具定义、Prompt 构建都复用 Eino / eino-ext 的官方组件，
// 业务代码只负责把商品检索、套装选择这些领域能力包装成 Eino 工具，
// 不再手写 OpenAI HTTP 协议或 function calling 消息循环。
package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	modelconfig "budgetmatch-sim/services/rpc/agent/internal/model"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

const (
	// defaultModel 是未显式配置模型名时使用的默认 OpenAI 模型。
	defaultModel = "gpt-4.1-mini"
	// requestTimeout 是单次模型请求的超时时间。
	requestTimeout = 60 * time.Second
)

// NewChatModel 根据配置创建 Eino ToolCallingChatModel。
//   - Provider 为空（noop）时返回 (nil, nil)，调用方据此走规则兜底；
//   - Provider 为 openai 时返回 eino-ext 官方 OpenAI ChatModel；
//   - 其他 Provider 暂不支持，返回错误。
//
// 该模型由 eino-ext 维护，原生支持真流式、重试和 Azure，业务侧无需再造 HTTP 客户端。
func NewChatModel(ctx context.Context, c modelconfig.Config) (model.ToolCallingChatModel, error) {
	switch c.ProviderName() {
	case "noop":
		return nil, nil
	case "openai":
		if strings.TrimSpace(c.APIKey) == "" {
			return nil, fmt.Errorf("openai api key is required when model provider is openai")
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  c.APIKey,
			Model:   modelName(c.Model),
			BaseURL: normalizeBaseURL(c.BaseURL),
			Timeout: requestTimeout,
		})
	default:
		return nil, fmt.Errorf("unsupported model provider %q", c.Provider)
	}
}

// modelName 返回有效的模型名，未配置时回落到默认模型。
func modelName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return defaultModel
}

// normalizeBaseURL 归一化自定义 BaseURL：底层 go-openai 客户端期望地址包含 /v1 后缀。
// 留空时交给组件使用 OpenAI 官方默认地址。
// 若用户已填写 /v1/chat/completions 等完整路径，则不再追加 /v1，避免重复。
func normalizeBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return ""
	}
	// 已包含 /v1 路径时直接返回，避免把 chat/completions 等路径重复拼接
	if strings.Contains(base, "/v1") {
		return base
	}
	return base + "/v1"
}
