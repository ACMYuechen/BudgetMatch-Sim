package modelconfig

import "strings"

// Config 描述 LLM 模型的连接配置。
type Config struct {
	Provider string `json:"provider,optional"` // Provider 模型提供商，如 openai
	Model    string `json:"model,optional"`    // Model 模型名称
	BaseURL  string `json:"baseUrl,optional"`  // BaseURL 模型 API 基础地址
	APIKey   string `json:"apiKey,optional"`   // APIKey 访问模型服务的密钥
}

// ProviderName 返回规范化的提供商名称；若未配置则返回 noop。
func (c Config) ProviderName() string {
	provider := strings.TrimSpace(strings.ToLower(c.Provider))
	if provider == "" {
		return "noop"
	}
	return provider
}

// Enabled 判断模型功能是否已启用。
func (c Config) Enabled() bool {
	return c.ProviderName() != "noop"
}
