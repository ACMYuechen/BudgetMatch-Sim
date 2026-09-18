package rag

import "fmt"

const (
	StrategyVectorFirst = "vector_first"
	StrategyHybridRRF   = "hybrid_rrf"
	MaxHybridWindow     = 64
	MaxHybridOutput     = 32 // the final live-check shortlist has the same bound
	RRFConstant         = 60
)

// RetrievalConfig only enables experiments explicitly. It does not retune the
// existing vector-first provider or its fixed-snapshot evaluation protocol.
type RetrievalConfig struct {
	Strategy      string `json:"strategy,optional"`
	InitialK      int    `json:"initialK,optional"`
	MaxK          int    `json:"maxK,optional"`
	TimeoutMillis int    `json:"timeoutMillis,optional"`
}

func (c RetrievalConfig) Normalize() RetrievalConfig {
	if c.Strategy == "" {
		c.Strategy = StrategyVectorFirst
	}
	if c.InitialK == 0 {
		c.InitialK = 16
	}
	if c.MaxK == 0 {
		c.MaxK = MaxHybridWindow
	}
	if c.TimeoutMillis == 0 {
		c.TimeoutMillis = 3000
	}
	return c
}

// Validate takes the raw RAG.TopK: zero uses its default, while an explicit
// negative value is rejected in hybrid mode instead of silently normalized.
func (c RetrievalConfig) Validate(outputK int) error {
	c = c.Normalize()
	if c.Strategy != StrategyVectorFirst && c.Strategy != StrategyHybridRRF {
		return fmt.Errorf("unsupported retrieval strategy")
	}
	if c.InitialK < 1 || c.MaxK < c.InitialK || c.MaxK > MaxHybridWindow ||
		c.TimeoutMillis < 100 || c.TimeoutMillis > 10000 {
		return fmt.Errorf("invalid retrieval experiment bounds")
	}
	if outputK == 0 {
		outputK = defaultTopK
	}
	if c.Strategy == StrategyHybridRRF && (outputK < 1 || outputK > MaxHybridOutput || outputK > c.InitialK) {
		return fmt.Errorf("hybrid output must be within 1..32 and no larger than initial window")
	}
	return nil
}
