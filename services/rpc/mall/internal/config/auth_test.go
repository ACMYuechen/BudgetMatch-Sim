package config

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/mapping"
)

func TestIndexSecretIsolation(t *testing.T) {
	var c Config
	c.JwtAuth.Secret = "user-jwt-unit-test-independent-secret"
	c.ServiceAuth.Secret = "payment-unit-test-independent-secret"
	require.NoError(t, c.ValidateIndexAuth()) // Old deployments stay usable; index access is denied.
	for _, secret := range []string{"short", " ", c.JwtAuth.Secret, c.ServiceAuth.Secret} {
		c.IndexAuth.Secret = secret
		require.Error(t, c.ValidateIndexAuth())
	}
	c.IndexAuth.Secret = "agent-index-unit-test-independent-secret"
	require.NoError(t, c.ValidateIndexAuth())
}

func TestMallIndexAuthTemplate(t *testing.T) {
	data, err := os.ReadFile("../../etc/config.yaml")
	require.NoError(t, err)
	// Substitute only a test value; never load .env or the process environment.
	for _, secret := range []string{"", "agent-index-unit-test-independent-secret"} {
		var c Config
		content := bytes.ReplaceAll(data, []byte("${AGENT_MALL_INDEX_SECRET}"), []byte(secret))
		require.NoError(t, conf.LoadFromYamlBytes(content, &c))
		require.Equal(t, secret, c.IndexAuth.Secret)
		require.NoError(t, c.ValidateIndexAuth())
	}
}

func TestComposeIndexSecretIsLimitedToAgentAndMall(t *testing.T) {
	data, err := os.ReadFile("../../../../../docker-compose.yml")
	require.NoError(t, err)
	type rpcService struct {
		Environment []string `json:"environment"`
	}
	var compose struct {
		Services struct {
			Agent   rpcService `json:"agent-rpc"`
			Mall    rpcService `json:"mall-rpc"`
			Payment rpcService `json:"payment-rpc"`
		} `json:"services"`
	}
	require.NoError(t, mapping.UnmarshalYamlBytes(data, &compose))
	const value = "AGENT_MALL_INDEX_SECRET=${AGENT_MALL_INDEX_SECRET:-}"
	require.Contains(t, compose.Services.Agent.Environment, value)
	require.Contains(t, compose.Services.Mall.Environment, value)
	require.NotContains(t, compose.Services.Payment.Environment, value)
	require.Equal(t, 2, bytes.Count(data, []byte(value)))
}
