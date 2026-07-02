package model

import "strings"

// defaultEmbeddingDim 是未配置向量维度时的默认值（text-embedding-3-small 的原生维度）。
const defaultEmbeddingDim = 1536

// EmbeddingConfig 描述 Embedding 模型的连接配置，Provider 语义与 Config 一致（空为 noop）。
// 注意 Embedding 与 LLM 是两套独立配置：部分 LLM 服务（如 DeepSeek）不提供 embeddings 接口。
type EmbeddingConfig struct {
	Provider   string `json:"provider,optional"`   // Provider 提供商，如 openai（OpenAI 兼容接口均可）
	Model      string `json:"model,optional"`      // Model 模型名称，如 text-embedding-3-small
	BaseURL    string `json:"baseUrl,optional"`    // BaseURL 模型 API 基础地址
	APIKey     string `json:"apiKey,optional"`     // APIKey 访问模型服务的密钥
	Dimensions int    `json:"dimensions,optional"` // Dimensions 向量维度，须与向量表一致，默认 1536
}

// ProviderName 返回规范化的提供商名称；若未配置则返回 noop。
func (c EmbeddingConfig) ProviderName() string {
	provider := strings.TrimSpace(strings.ToLower(c.Provider))
	if provider == "" {
		return "noop"
	}
	return provider
}

// Enabled 判断 Embedding 功能是否已启用。
func (c EmbeddingConfig) Enabled() bool {
	return c.ProviderName() != "noop"
}

// Dim 返回有效的向量维度，未配置时回落默认值。
func (c EmbeddingConfig) Dim() int {
	if c.Dimensions > 0 {
		return c.Dimensions
	}
	return defaultEmbeddingDim
}

// NormalizeBaseURL 归一化自定义 BaseURL：底层 go-openai 客户端期望地址包含 /v1 后缀。
// 留空时交给组件使用官方默认地址；已含 /v1 路径时直接返回，避免重复拼接。
func NormalizeBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return ""
	}
	if strings.Contains(base, "/v1") {
		return base
	}
	return base + "/v1"
}
