package llm

import "strings"

type Config struct {
	Provider string `json:"provider,optional"`
	Model    string `json:"model,optional"`
	BaseURL  string `json:"baseUrl,optional"`
	APIKey   string `json:"apiKey,optional"`
}

func (c Config) ProviderName() string {
	provider := strings.TrimSpace(strings.ToLower(c.Provider))
	if provider == "" {
		return "noop"
	}
	return provider
}

func (c Config) Enabled() bool {
	return c.ProviderName() != "noop"
}
