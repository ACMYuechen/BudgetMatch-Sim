package rag

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetrievalExperimentDefaultsAndBounds(t *testing.T) {
	cfg := (RetrievalConfig{}).Normalize()
	require.Equal(t, StrategyVectorFirst, cfg.Strategy)
	require.Equal(t, 16, cfg.InitialK)
	require.Equal(t, 64, cfg.MaxK)
	require.Equal(t, 3000, cfg.TimeoutMillis)
	require.NoError(t, cfg.Validate(10))
	for _, cfg := range []RetrievalConfig{
		{Strategy: "hybird_rrf"}, {InitialK: -1}, {InitialK: 65}, {InitialK: 20, MaxK: 10},
		{MaxK: 65}, {TimeoutMillis: 99}, {TimeoutMillis: 10001},
	} {
		require.Error(t, cfg.Validate(10), "%+v", cfg)
	}
	hybrid := RetrievalConfig{Strategy: StrategyHybridRRF, InitialK: 32}
	for _, k := range []int{-1, 33, 64} {
		require.Error(t, hybrid.Validate(k))
	}
	for _, k := range []int{0, 1, 10, 32} {
		require.NoError(t, hybrid.Validate(k))
	}
	require.Error(t, (RetrievalConfig{Strategy: StrategyHybridRRF, InitialK: 8}).Validate(10))
	require.NoError(t, cfg.Validate(100), "do not retune the vector-first baseline")
}
