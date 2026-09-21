package rag

import (
	"context"
	"errors"
	"testing"

	modelconfig "budgetmatch-sim/services/rpc/agent/internal/model"
	"budgetmatch-sim/services/rpc/agent/model/product_vectors"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingProfileUsesEffectiveConfigWithoutCredentials(t *testing.T) {
	base := modelconfig.EmbeddingConfig{Provider: " OPENAI ", APIKey: "PRIVATE_KEY_A"}
	p := EmbeddingProfile(base)
	require.NoError(t, p.Validate())
	require.Equal(t, 1536, p.Dimensions)
	require.NotContains(t, p.Fingerprint, "PRIVATE")
	normalized := base
	normalized.Provider, normalized.Model, normalized.Dimensions, normalized.APIKey = "openai", defaultEmbeddingModel, 1536, "PRIVATE_KEY_B"
	require.Equal(t, p, EmbeddingProfile(normalized))
	for _, change := range []func(*modelconfig.EmbeddingConfig){
		func(c *modelconfig.EmbeddingConfig) { c.Model = "different-model" },
		func(c *modelconfig.EmbeddingConfig) { c.BaseURL = "https://embedding.test" },
		func(c *modelconfig.EmbeddingConfig) { c.Provider = "different-provider" },
		func(c *modelconfig.EmbeddingConfig) { c.Dimensions = 3 },
	} {
		changed := normalized
		change(&changed)
		require.NotEqual(t, p.Fingerprint, EmbeddingProfile(changed).Fingerprint)
	}
	base.BaseURL, normalized.BaseURL = " https://embedding.test/ ", "https://embedding.test/v1"
	require.Equal(t, EmbeddingProfile(base), EmbeddingProfile(normalized))
}

func TestRetrieverCannotOverrideBoundModel(t *testing.T) {
	calls := 0
	embedder := embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) { calls++; return testVectors(texts), nil })
	r := NewRetriever(&Store{model: &fakeVectorModel{}, embedder: embedder, cfg: Config{TopK: 10}, dim: 3})
	_, err := r.Retrieve(context.Background(), "test", retriever.WithEmbedding(embedder))
	require.ErrorContains(t, err, "embedding overrides")
	require.Zero(t, calls)
	_, err = r.Retrieve(context.Background(), "test", retriever.WithEmbedding(nil))
	require.Error(t, err)
	require.Zero(t, calls)
	_, err = r.Retrieve(context.Background(), "test", retriever.WithTopK(2))
	require.NoError(t, err)
	require.Equal(t, 1, calls, "non-model Eino options remain supported")
}

type initializationModel struct {
	fakeVectorModel
	err         error
	initialized bool
}

func (m *initializationModel) Initialize(ctx context.Context) error {
	m.initialized = true
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.err
}

func TestStoreDoesNotAdmitMismatchedOrUnboundIndex(t *testing.T) {
	for _, failure := range []error{product_vectors.ErrProfileMismatch, product_vectors.ErrUnboundIndex, product_vectors.ErrSyncBusy} {
		model := &initializationModel{err: failure}
		calls := 0
		embedder := embedderFunc(func(context.Context, []string) ([][]float64, error) {
			calls++
			return nil, errors.New("must not embed")
		})
		store, err := NewStore(context.Background(), model, embedder, Config{})
		require.ErrorIs(t, err, failure)
		require.Nil(t, store)
		require.True(t, model.initialized)
		require.Zero(t, calls)
	}
	model := &initializationModel{}
	store, err := NewStore(context.Background(), model, embedderFunc(func(_ context.Context, texts []string) ([][]float64, error) { return testVectors(texts), nil }), Config{})
	require.NoError(t, err)
	require.Equal(t, model.IndexProfile().Fingerprint, store.Fingerprint())
	require.Equal(t, model.IndexProfile().Dimensions, store.dim)
}
