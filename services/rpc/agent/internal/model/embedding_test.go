package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddingConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		name, provider, model, key, wantErr string
		dimensions                          int
	}{
		{name: "disabled_retained_settings", model: "BAAI/bge-m3", dimensions: -1},
		{name: "explicit_noop", provider: " NOOP ", model: "BAAI/bge-m3"},
		{name: "legacy_default", provider: "openai", key: "fixture-secret"},
		{name: "legacy_explicit", provider: "openai", key: "fixture-secret", dimensions: 1536},
		{name: "generic_model", provider: "openai", model: "fixture-model", key: "fixture-secret", dimensions: 128},
		{name: "bge_m3", provider: " OPENAI ", model: " BAAI/bge-m3 ", key: "fixture-secret", dimensions: 1024},
		{name: "bge_missing_dimension", provider: "openai", model: "BAAI/bge-m3", key: "fixture-secret", wantErr: "EMBEDDING_DIMENSIONS"},
		{name: "bge_wrong_dimension", provider: "openai", model: "BAAI/bge-m3", key: "fixture-secret", dimensions: 1536, wantErr: "EMBEDDING_DIMENSIONS"},
		{name: "negative", provider: "openai", key: "fixture-secret", dimensions: -1, wantErr: "negative"},
		{name: "missing_key", provider: "openai", wantErr: "api key is required"},
		{name: "blank_key", provider: "openai", key: " \t", wantErr: "api key is required"},
		{name: "unsupported_provider", provider: "fixture-secret", wantErr: "unsupported embedding provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := EmbeddingConfig{Provider: tc.provider, Model: tc.model, APIKey: tc.key,
				BaseURL: "https://fixture-secret.invalid/v1", Dimensions: tc.dimensions}
			err := cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			require.NotContains(t, err.Error(), "fixture-secret")
		})
	}
}
