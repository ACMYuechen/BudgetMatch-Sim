package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	modelconfig "budgetmatch-sim/services/rpc/agent/internal/model"
	"budgetmatch-sim/services/rpc/agent/model/product_vectors"
)

// EmbeddingProfile uses the same normalization/defaults as NewEmbedder, but never
// includes APIKey. Credential rotation alone must not invalidate the index.
// Endpoint differences are conservative: an empty default and an explicit URL
// have distinct identities even if an operator knows they route to one service.
func EmbeddingProfile(c modelconfig.EmbeddingConfig) product_vectors.Profile {
	data, _ := json.Marshal(struct {
		Version                   int
		Provider, Endpoint, Model string
		Dimensions                int
	}{1, c.ProviderName(), modelconfig.NormalizeBaseURL(c.BaseURL), embeddingModelName(c.Model), c.Dim()})
	digest := sha256.Sum256(data)
	return product_vectors.Profile{Fingerprint: hex.EncodeToString(digest[:]), Dimensions: c.Dim()}
}
