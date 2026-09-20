package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider string
		model    string
		thinking string
		key      string
		wantErr  string
	}{
		{name: "empty_noop"},
		{name: "explicit_noop", provider: " NOOP ", model: "deepseek-flash", thinking: "retained-unused-value"},
		{name: "provider_removed", model: "deepseek-flash", thinking: "disabled"},
		{name: "legacy_default", provider: "openai", key: "fixture-secret"},
		{name: "legacy_explicit", provider: "openai", model: "local-fixture", key: "fixture-secret"},
		{name: "legacy_blank_mode", provider: "openai", model: "local-fixture", thinking: " \t", key: "fixture-secret"},
		{name: "flash", provider: "openai", model: "deepseek-flash", thinking: "disabled", key: "fixture-secret"},
		{name: "trimmed", provider: " OPENAI ", model: " deepseek-flash ", thinking: " disabled ", key: "fixture-secret"},
		{name: "flash_missing_mode", provider: "openai", model: "deepseek-flash", key: "fixture-secret", wantErr: "Model.Thinking=disabled"},
		{name: "flash_blank_mode", provider: "openai", model: "deepseek-flash", thinking: " \t", key: "fixture-secret", wantErr: "Model.Thinking=disabled"},
		{name: "flash_enabled", provider: "openai", model: "deepseek-flash", thinking: "enabled", key: "fixture-secret", wantErr: "Model.Thinking=disabled"},
		{name: "flash_typo", provider: "openai", model: "deepseek-flash", thinking: "disabeld-fixture-secret", key: "fixture-secret", wantErr: "Model.Thinking=disabled"},
		{name: "other_model_mode", provider: "openai", model: "local-fixture", thinking: "disabled", key: "fixture-secret", wantErr: "only for deepseek-flash"},
		{name: "default_model_mode", provider: "openai", thinking: "disabled", key: "fixture-secret", wantErr: "only for deepseek-flash"},
		{name: "unsupported_provider", provider: "fixture-secret", key: "fixture-secret", wantErr: "unsupported model provider"},
		{name: "missing_key", provider: "openai", model: "deepseek-flash", thinking: "disabled", wantErr: "api key is required"},
		{name: "blank_key", provider: "openai", key: " \t", wantErr: "api key is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Provider: tc.provider, Model: tc.model, Thinking: tc.thinking,
				APIKey: tc.key, BaseURL: "https://fixture-secret.invalid/v1"}
			err := cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			require.NotContains(t, err.Error(), "fixture-secret", "validation errors must not echo configuration values")
		})
	}
}
