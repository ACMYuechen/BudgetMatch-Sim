package config

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestIndexAuthRequiredOnlyForConfiguredRAG(t *testing.T) {
	var c Config
	require.NoError(t, c.ValidateIndexAuth())
	c.MallRpc.Endpoints = []string{"unused.test:10005"}
	c.Database.DSN = "not-a-real-database"
	require.NoError(t, c.ValidateIndexAuth())
	c.Embedding.Provider = "test-only"
	require.True(t, c.RAGConfigured())
	require.Error(t, c.ValidateIndexAuth())
	c.IndexAuth.Secret = "short"
	require.Error(t, c.ValidateIndexAuth())
	c.IndexAuth.Secret = "agent-index-unit-test-independent-secret"
	require.NoError(t, c.ValidateIndexAuth())
	c.JwtAuth.Secret = c.IndexAuth.Secret
	require.Error(t, c.ValidateIndexAuth())
}

func TestAgentIndexAuthTemplate(t *testing.T) {
	data, err := os.ReadFile("../../etc/config.yaml")
	require.NoError(t, err)
	for _, enabled := range []bool{false, true} {
		provider := ""
		if enabled {
			provider = "test-only"
		}
		for _, secret := range []string{"", "agent-index-unit-test-independent-secret"} {
			var c Config
			content := bytes.ReplaceAll(data, []byte("${AGENT_MALL_INDEX_SECRET}"), []byte(secret))
			content = bytes.ReplaceAll(content, []byte("${EMBEDDING_PROVIDER}"), []byte(provider))
			require.NoError(t, conf.LoadFromYamlBytes(content, &c))
			require.Equal(t, secret, c.IndexAuth.Secret)
			require.Equal(t, enabled, c.RAGConfigured())
			require.Equal(t, !enabled || secret != "", c.ValidateIndexAuth() == nil)
		}
	}
}
