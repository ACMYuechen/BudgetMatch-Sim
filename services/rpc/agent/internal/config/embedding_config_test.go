package config

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestEmbeddingTemplateDimensionsAndLegacyDefaults(t *testing.T) {
	template, err := os.ReadFile("../../etc/config.yaml")
	require.NoError(t, err)
	require.Contains(t, string(template), "Dimensions: 1536")
	const caseVariable = "BUDGETMATCH_TEST_EMBEDDING_TEMPLATE_CASE"
	selected := os.Getenv(caseVariable)
	for _, tc := range []struct {
		name, model, dimensions string
		wantDim                 int
		parseError, invalid     bool
		yamlOverride, emptyEnv  bool
	}{
		{name: "missing_legacy", model: "text-embedding-3-small", wantDim: 1536},
		{name: "empty_legacy", model: "text-embedding-3-small", wantDim: 1536, emptyEnv: true},
		{name: "zero_legacy", model: "text-embedding-3-small", dimensions: "0", wantDim: 1536},
		{name: "explicit_legacy", model: "text-embedding-3-small", dimensions: "1536", wantDim: 1536},
		{name: "bge_m3", model: "BAAI/bge-m3", dimensions: "1024", wantDim: 1024},
		{name: "bge_missing_dimension", model: "BAAI/bge-m3", wantDim: 1536, invalid: true},
		{name: "bge_wrong_dimension", model: "BAAI/bge-m3", dimensions: "1536", wantDim: 1536, invalid: true},
		{name: "negative_dimension", model: "local-fixture", dimensions: "-1", wantDim: 1536, invalid: true},
		{name: "invalid_number", model: "local-fixture", dimensions: "invalid", parseError: true},
		{name: "yaml_override", model: "BAAI/bge-m3", wantDim: 1024, yamlOverride: true},
	} {
		if selected != "" && selected != tc.name {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			if selected == "" {
				// go-zero caches env-tag values for the lifetime of the process.
				// Test each startup configuration in a fresh process, using no
				// inherited credentials or external dependency configuration.
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEmbeddingTemplateDimensionsAndLegacyDefaults$", "-test.count=1")
				command.Env = []string{caseVariable + "=" + tc.name, "GOMAXPROCS=2", "GORACE=atexit_sleep_ms=0"}
				if tc.dimensions != "" || tc.emptyEnv {
					command.Env = append(command.Env, "EMBEDDING_DIMENSIONS="+tc.dimensions)
				}
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				return
			}
			values := map[string]string{
				"EMBEDDING_PROVIDER": "openai", "EMBEDDING_MODEL": tc.model,
				"EMBEDDING_API_KEY":  "fixture-key",
				"EMBEDDING_BASE_URL": "http://127.0.0.1:1/v1", "ETCD_HOSTS": "127.0.0.1:1",
				"JWT_SECRET": "fixture-user-secret", "AGENT_MALL_INDEX_SECRET": "fixture-index-independent-secret-32-bytes",
			}
			// Resolve string placeholders from fixtures; only the parent-provided
			// public dimension value is read through the numeric env tag.
			data := os.Expand(string(template), func(key string) string { return values[key] })
			if tc.yamlOverride {
				data = strings.Replace(data, "Dimensions: 1536", "Dimensions: 1024", 1)
			}
			var cfg Config
			err := conf.LoadFromYamlBytes([]byte(data), &cfg)
			if tc.parseError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantDim, cfg.Embedding.Dim())
			require.Equal(t, tc.invalid, cfg.Embedding.Validate() != nil)
			require.Equal(t, "disabled", cfg.DemandExecution.Mode)
		})
	}
}
