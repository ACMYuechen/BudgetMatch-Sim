package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestModelTemplateExplicitThinkingAndLegacyCompatibility(t *testing.T) {
	template, err := os.ReadFile("../../etc/config.yaml")
	require.NoError(t, err)
	require.Contains(t, string(template), `Thinking: "${LLM_THINKING}"`)
	for _, tc := range []struct {
		name, provider, model, thinking string
		wantErr                         bool
	}{
		{name: "flash", provider: "openai", model: "deepseek-flash", thinking: "disabled"},
		{name: "flash_missing_key", provider: "openai", model: "deepseek-flash", wantErr: true},
		{name: "legacy_generic", provider: "openai", model: "local-fixture"},
		{name: "noop_with_residual_settings", model: "deepseek-flash", thinking: "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Replace only these placeholders; never read the real environment or
			// initialize any of the template's database/RPC clients.
			data := strings.NewReplacer(
				"${LLM_PROVIDER}", tc.provider, "${LLM_MODEL}", tc.model,
				"${LLM_THINKING}", tc.thinking, "${LLM_API_KEY}", "fixture-key",
				"${LLM_BASE_URL}", "http://127.0.0.1:1/v1",
			).Replace(string(template))
			var cfg Config
			require.NoError(t, conf.LoadFromYamlBytes([]byte(data), &cfg))
			require.Equal(t, tc.model, cfg.Model.Model)
			require.Equal(t, tc.thinking, cfg.Model.Thinking)
			if tc.wantErr {
				require.ErrorContains(t, cfg.Model.Validate(), "LLM_THINKING")
			} else {
				require.NoError(t, cfg.Model.Validate())
			}
		})
	}
}
