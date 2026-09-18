package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestDemandExecutionDefaultsAndMallIsolation(t *testing.T) {
	require.NoError(t, (Config{}).ValidateDemandExecution())
	data, err := os.ReadFile("../../etc/config.yaml")
	require.NoError(t, err)
	var c Config
	require.NoError(t, conf.LoadFromYamlBytes(data, &c))
	require.Equal(t, "disabled", c.DemandExecution.Mode)
	require.NoError(t, c.ValidateDemandExecution())
	c.DemandExecution.Mode = "demo"
	require.Error(t, c.ValidateDemandExecution(), "template has Mall configured")
	for _, mode := range []string{"live", "DEMO", "invalid"} {
		c := Config{DemandExecution: DemandExecutionConfig{Mode: mode}}
		require.Error(t, c.ValidateDemandExecution())
	}
	c = Config{DemandExecution: DemandExecutionConfig{Mode: "demo"}}
	require.NoError(t, c.ValidateDemandExecution())
	c.MallRpc.Endpoints = []string{"unused"}
	require.Error(t, c.ValidateDemandExecution())
	c.MallRpc.Endpoints = nil
	c.MallRpc.Target = "unused"
	require.Error(t, c.ValidateDemandExecution())
	c.DemandExecution.Mode = "mall"
	require.NoError(t, c.ValidateDemandExecution())
	c.MallRpc.Target = ""
	require.Error(t, c.ValidateDemandExecution(), "Mall mode must never silently use mock")
	c.MallRpc.Endpoints = []string{"unused"}
	require.NoError(t, c.ValidateDemandExecution())
}
