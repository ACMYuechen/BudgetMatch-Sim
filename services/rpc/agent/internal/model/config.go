package model

import (
	"errors"
	"strings"
)

// Config 描述 LLM 模型的连接配置。
type Config struct {
	Provider string `json:"provider,optional"` // Provider 模型提供商，如 openai
	Model    string `json:"model,optional"`    // Model 模型名称
	BaseURL  string `json:"baseUrl,optional"`  // BaseURL 模型 API 基础地址
	APIKey   string `json:"apiKey,optional"`   // APIKey 访问模型服务的密钥
	Thinking string `json:"thinking,optional"` // Thinking 仅为 deepseek-flash 显式设置 disabled；其他模型留空
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

// Validate rejects unsupported settings before any external initialization.
// Flash must explicitly use the non-thinking mode covered by acceptance; this
// is not a generic extra-body escape hatch or a provider-side capability probe.
func (c Config) Validate() error {
	switch c.ProviderName() {
	case "noop":
		return nil // Disabling the provider must also work with retained settings.
	case "openai":
		if strings.TrimSpace(c.APIKey) == "" {
			return errors.New("openai api key is required when model provider is openai")
		}
	default:
		return errors.New("unsupported model provider")
	}
	thinking := strings.TrimSpace(c.Thinking)
	if strings.TrimSpace(c.Model) == "deepseek-flash" {
		if thinking != "disabled" {
			return errors.New("deepseek-flash requires Model.Thinking=disabled (LLM_THINKING)")
		}
	} else if thinking != "" {
		return errors.New("Model.Thinking is supported only for deepseek-flash; leave it empty for other models")
	}
	return nil
}
