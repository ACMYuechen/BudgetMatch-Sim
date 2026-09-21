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

func TestDemandRAGRequiresExplicitDependenciesAndBoundedWindow(t *testing.T) {
	c := Config{DemandExecution: DemandExecutionConfig{Mode: "mall", Retrieval: "rag"}}
	c.MallRpc.Endpoints = []string{"unused"}
	require.Error(t, c.ValidateDemandExecution())
	c.Database.DSN = "unused"
	require.Error(t, c.ValidateDemandExecution())
	c.Embedding.Provider = "openai"
	require.NoError(t, c.ValidateDemandExecution())
	for _, topK := range []int{-1, 33, 1024} {
		c.RAG.TopK = topK
		require.Error(t, c.ValidateDemandExecution())
	}
	c.RAG.TopK = 32
	require.NoError(t, c.ValidateDemandExecution())
	c.DemandExecution.Retrieval = "rga"
	require.Error(t, c.ValidateDemandExecution())
	c.DemandExecution = DemandExecutionConfig{Mode: "disabled", Retrieval: "rag"}
	c.Database.DSN, c.Embedding.Provider = "", ""
	require.NoError(t, c.ValidateDemandExecution(), "disabling execution does not require retained RAG dependencies")
	c.DemandExecution.Mode = "demo"
	c.MallRpc.Endpoints = nil
	require.Error(t, c.ValidateDemandExecution(), "demo cannot use real retrieval")
}
