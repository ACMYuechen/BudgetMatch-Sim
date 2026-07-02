package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	modelconfig "budgetmatch-sim/services/rpc/agent/internal/model"

	openaiembed "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
)

const (
	// defaultEmbeddingModel 是未显式配置模型名时使用的默认 embedding 模型。
	defaultEmbeddingModel = "text-embedding-3-small"
	// embedTimeout 是单次 embedding 请求的超时时间。
	embedTimeout = 30 * time.Second
)

// NewEmbedder 根据配置创建 Eino Embedder，语义与 llm.NewChatModel 一致：
//   - Provider 为空（noop）时返回 (nil, nil)，调用方据此关闭 RAG；
//   - Provider 为 openai 时返回 eino-ext 官方 OpenAI Embedder（兼容 DashScope 等 OpenAI 兼容接口）；
//   - 其他 Provider 暂不支持，返回错误。
func NewEmbedder(ctx context.Context, c modelconfig.EmbeddingConfig) (embedding.Embedder, error) {
	switch c.ProviderName() {
	case "noop":
		return nil, nil
	case "openai":
		if strings.TrimSpace(c.APIKey) == "" {
			return nil, fmt.Errorf("embedding api key is required when provider is openai")
		}
		dims := c.Dim()
		return openaiembed.NewEmbedder(ctx, &openaiembed.EmbeddingConfig{
			APIKey:     c.APIKey,
			Model:      embeddingModelName(c.Model),
			BaseURL:    modelconfig.NormalizeBaseURL(c.BaseURL),
			Dimensions: &dims,
			Timeout:    embedTimeout,
		})
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", c.Provider)
	}
}

// embeddingModelName 返回有效的模型名，未配置时回落默认模型。
func embeddingModelName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return defaultEmbeddingModel
}
