package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	modelconfig "budgetmatch-sim/services/rpc/agent/internal/model"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingRequestFixedAndConfigurableDimensions(t *testing.T) {
	for _, tc := range []struct {
		name, model   string
		dim, expected int
		omit          bool
	}{
		{name: "bge_m3", model: "BAAI/bge-m3", dim: 1024, expected: 1024, omit: true},
		{name: "trimmed_bge_m3", model: " BAAI/bge-m3 ", dim: 1024, expected: 1024, omit: true},
		{name: "legacy_default", expected: 1536},
		{name: "legacy_explicit", model: "text-embedding-3-small", dim: 1536, expected: 1536},
		{name: "custom_dimensions", model: "fixture-model", dim: 128, expected: 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
					t.Error("unexpected embedding route or authentication")
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				var payload struct {
					Model          string   `json:"model"`
					Input          []string `json:"input"`
					Dimensions     *int     `json:"dimensions"`
					EncodingFormat string   `json:"encoding_format"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&payload); err != nil {
					t.Error("invalid embedding request body")
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				if payload.Model != embeddingModelName(tc.model) || payload.EncodingFormat != "float" || len(payload.Input) != 2 ||
					(tc.omit && payload.Dimensions != nil) || (!tc.omit && (payload.Dimensions == nil || *payload.Dimensions != tc.expected)) {
					t.Error("unexpected embedding parameters")
					http.Error(w, "unsupported parameters", http.StatusBadRequest)
					return
				}
				data := make([]map[string]any, len(payload.Input))
				for i := range data {
					vector := make([]float64, tc.expected)
					vector[i] = 1
					data[i] = map[string]any{"object": "embedding", "index": i, "embedding": vector}
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": payload.Model, "data": data,
					"usage": map[string]int{"prompt_tokens": 2, "total_tokens": 2}}); err != nil {
					t.Error("failed to encode embedding fixture")
				}
			}))
			defer server.Close()
			cfg := modelconfig.EmbeddingConfig{Provider: "openai", Model: tc.model, APIKey: "fixture-secret",
				BaseURL: server.URL, Dimensions: tc.dim}
			embedder, err := NewEmbedder(context.Background(), cfg)
			require.NoError(t, err)
			require.Zero(t, calls.Load(), "constructor must not call the provider")
			vectors, err := embedder.EmbedStrings(context.Background(), []string{"fixture mouse", "fixture keyboard"})
			require.NoError(t, err)
			require.EqualValues(t, 1, calls.Load())
			require.Len(t, vectors, 2)
			require.Len(t, vectors[0], tc.expected)
			require.Len(t, vectors[1], tc.expected)
			require.Equal(t, 1.0, vectors[0][0])
			require.Equal(t, 1.0, vectors[1][1])
			require.Equal(t, tc.expected, EmbeddingProfile(cfg).Dimensions, "API omission must not change index dimensions")
		})
	}
}

func TestEmbeddingRejectsWrongBGEDimensionsBeforeIO(t *testing.T) {
	embedder, err := NewEmbedder(context.Background(), modelconfig.EmbeddingConfig{
		Provider: "openai", Model: "BAAI/bge-m3", Dimensions: 1536,
		APIKey: "fixture-secret", BaseURL: "http://127.0.0.1:1/v1",
	})
	require.ErrorContains(t, err, "EMBEDDING_DIMENSIONS")
	require.Nil(t, embedder)
	embedder, err = NewEmbedder(context.Background(), modelconfig.EmbeddingConfig{Model: "BAAI/bge-m3"})
	require.NoError(t, err)
	require.Nil(t, embedder)
}
