package svc

import (
	"context"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type noIOVector struct{}

func (noIOVector) Retrieve(context.Context, string, ...retriever.Option) ([]*schema.Document, error) {
	panic("construction must not retrieve")
}

func TestRetrievalStrategyDefaultIsUnchangedAndHybridIsExplicit(t *testing.T) {
	provider, err := newRAGProvider(rag.Config{}, noIOVector{}, tools.NewMockProductProvider())
	require.NoError(t, err)
	require.IsType(t, &tools.RAGProductProvider{}, provider)
	cfg := rag.Config{Retrieval: rag.RetrievalConfig{Strategy: rag.StrategyHybridRRF}}
	_, err = newRAGProvider(cfg, noIOVector{}, tools.NewMockProductProvider())
	require.Error(t, err)
	provider, err = newRAGProvider(cfg, noIOVector{}, tools.NewMallProductProvider(nil))
	require.NoError(t, err)
	require.IsType(t, &tools.HybridProductProvider{}, provider)
}

func TestInvalidRetrievalConfigStopsBeforeExternalInitialization(t *testing.T) {
	for _, strategy := range []string{"hybird_rrf", rag.StrategyHybridRRF} {
		var cfg config.Config
		cfg.RAG.Retrieval.Strategy = strategy
		cfg.Database.DSN = "invalid-dsn-must-not-be-opened"
		err := cfg.ValidateRetrieval()
		require.Error(t, err)
		require.PanicsWithError(t, err.Error(), func() { NewServiceContext(cfg) })
	}
	var cfg config.Config
	cfg.RAG.Retrieval.Strategy = rag.StrategyHybridRRF
	cfg.Database.DSN = "not-used"
	cfg.MallRpc.Endpoints = []string{"unused.invalid:10005"}
	cfg.Embedding.Provider = "not-used"
	require.NoError(t, cfg.ValidateRetrieval())
	require.Error(t, cfg.ValidateIndexAuth(), "retrieval mode cannot bypass index credential checks")
}

func TestHybridNegativeTopKIsRejectedBeforeNormalization(t *testing.T) {
	cfg := rag.Config{TopK: -1, Retrieval: rag.RetrievalConfig{Strategy: rag.StrategyHybridRRF}}
	keyword := tools.NewMallProductProvider(nil)
	_, err := newRAGProvider(cfg, noIOVector{}, keyword)
	require.Error(t, err)
	_, err = tools.NewHybridProductProvider(noIOVector{}, keyword, cfg)
	require.Error(t, err)
	c := config.Config{RAG: cfg}
	require.Error(t, c.ValidateRetrieval())
	cfg.Retrieval.Strategy = rag.StrategyVectorFirst
	_, err = newRAGProvider(cfg, noIOVector{}, keyword)
	require.NoError(t, err, "preserve legacy normalization in the baseline")
}
