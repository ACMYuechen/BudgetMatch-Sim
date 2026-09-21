package svc

import (
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/model"
	"github.com/stretchr/testify/require"
)

func TestInvalidBGEDimensionsRejectedBeforeDatabaseInitialization(t *testing.T) {
	c := config.Config{}
	c.Database.DSN = "invalid database configuration must not be used"
	c.MallRpc.Endpoints = []string{"127.0.0.1:1"}
	c.IndexAuth.Secret = "fixture-index-independent-secret-32-bytes"
	c.Embedding = model.EmbeddingConfig{Provider: "openai", Model: "BAAI/bge-m3", APIKey: "fixture-secret"}
	require.PanicsWithError(t, "BAAI/bge-m3 requires Embedding.Dimensions=1024 (EMBEDDING_DIMENSIONS)", func() {
		NewServiceContext(c)
	})
}
