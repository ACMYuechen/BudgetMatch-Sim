package llm

import "fmt"

func NewClient(c Config) (Client, error) {
	switch c.ProviderName() {
	case "noop":
		return NewNoopClient(), nil
	case "openai":
		if c.APIKey == "" {
			return nil, fmt.Errorf("openai api key is required when model provider is openai")
		}
		return NewOpenAIClient(c), nil
	default:
		return nil, fmt.Errorf("unsupported llm provider %q", c.Provider)
	}
}
