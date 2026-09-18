package config

import (
	"bytes"
	"os"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestRetrievalTemplateAndLegacyDefaults(t *testing.T) {
	data, err := os.ReadFile("../../etc/config.yaml")
	require.NoError(t, err)
	var c Config
	require.NoError(t, conf.LoadFromYamlBytes(data, &c))
	require.Equal(t, rag.StrategyVectorFirst, c.RAG.Retrieval.Strategy)
	require.Equal(t, rag.RetrievalConfig{}.Normalize(), c.RAG.Retrieval)
	require.NoError(t, c.ValidateRetrieval())

	data = bytes.ReplaceAll(data, []byte("Strategy: vector_first"), []byte("Strategy: hybrid_rrf"))
	require.NoError(t, conf.LoadFromYamlBytes(data, &c))
	require.Equal(t, rag.StrategyHybridRRF, c.RAG.Retrieval.Strategy)
	require.NoError(t, c.RAG.Retrieval.Validate(c.RAG.TopK))

	var legacy struct{ RAG rag.Config }
	require.NoError(t, conf.LoadFromYamlBytes([]byte("RAG:\n  TopK: 10\n"), &legacy))
	require.Equal(t, rag.RetrievalConfig{}.Normalize(), legacy.RAG.Retrieval.Normalize())
	require.NoError(t, legacy.RAG.Retrieval.Validate(legacy.RAG.TopK))
}
