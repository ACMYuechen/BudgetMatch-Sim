package config

import (
	"os"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestRAGSyncTimeoutTemplateAndLegacyDefaults(t *testing.T) {
	data, err := os.ReadFile("../../etc/config.yaml")
	require.NoError(t, err)
	var c Config
	require.NoError(t, conf.LoadFromYamlBytes(data, &c))
	require.Equal(t, 300, c.RAG.SyncTimeoutSeconds)
	require.Equal(t, 5, c.RAG.SyncStopTimeoutSeconds)
	var legacy struct{ RAG rag.Config }
	require.NoError(t, conf.LoadFromYamlBytes([]byte("RAG:\n  SyncIntervalSeconds: -1\n"), &legacy))
	normalized := legacy.RAG.Normalize()
	require.Equal(t, -1, normalized.SyncIntervalSeconds)
	require.Equal(t, 300, normalized.SyncTimeoutSeconds)
	require.Equal(t, 5, normalized.SyncStopTimeoutSeconds)
}
