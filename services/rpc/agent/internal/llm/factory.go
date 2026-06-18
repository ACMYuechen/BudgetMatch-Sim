package llm

import "fmt"

func NewClient(c Config) (Client, error) {
	switch c.ProviderName() {
	case "noop":
		return NewNoopClient(), nil
	case "openai":
		if c.APIKey == "" {
			return NewNoopClient(), nil
		}
		return nil, fmt.Errorf("openai llm provider is not implemented yet")
	default:
		return nil, fmt.Errorf("unsupported llm provider %q", c.Provider)
	}
}
