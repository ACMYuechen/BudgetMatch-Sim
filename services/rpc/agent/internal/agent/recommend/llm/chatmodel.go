// Package llm 基于 Eino ReAct 实现推荐 Agent 的 LLM 编排能力。
// 模型接入、工具定义、Prompt 构建都复用 Eino / eino-ext 的官方组件，
// 业务代码只负责把商品检索、套装选择这些领域能力包装成 Eino 工具，
// 不再手写 OpenAI HTTP 协议或 function calling 消息循环。
package llm

import (
	"context"
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
// 沿用已锁定的 SDK；不在此添加 HTTP 重试、自动模型名映射或任意额外请求参数。
func NewChatModel(ctx context.Context, c modelconfig.Config) (model.ToolCallingChatModel, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !c.Enabled() {
		return nil, nil
	}
	cfg := &openai.ChatModelConfig{
		APIKey:  c.APIKey,
		Model:   modelName(c.Model),
		BaseURL: modelconfig.NormalizeBaseURL(c.BaseURL),
		Timeout: requestTimeout,
	}
	if strings.TrimSpace(c.Thinking) == "disabled" {
		cfg.ExtraFields = map[string]any{"thinking": map[string]any{"type": "disabled"}}
	}
	return openai.NewChatModel(ctx, cfg)
}

// modelName 返回有效的模型名，未配置时回落到默认模型。
func modelName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return defaultModel
}
