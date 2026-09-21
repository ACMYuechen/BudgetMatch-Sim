package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

const retrievalProtocol = "bounded-rank-replay-v1"

var retrievalStrategies = [...]string{"keyword_only", "vector_only", "vector_first", "hybrid_rrf"}

type RetrievalProduct struct {
	ID         string `json:"id"`
	ProductID  string `json:"product_id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	Stock      int64  `json:"stock"`
	Sold       int64  `json:"sold"`
}

// IDs are prerecorded ranks, not computed from relevance labels. A wider
// request reads a longer prefix of exactly the same immutable list.
type RetrievalLane struct {
	IDs        []string `json:"ids"`
	Error      string   `json:"error,omitempty"`
	WiderError string   `json:"wider_error,omitempty"`
}

type RetrievalCase struct {
	ID               string            `json:"id"`
	Kind             string            `json:"kind"` // quality / fault; no held-out claim
	Input            TurnInput         `json:"input"`
	Keyword          RetrievalLane     `json:"keyword"`
	Vector           RetrievalLane     `json:"vector"`
	RelevantSKUs     []string          `json:"relevant_skus"`
	ExpectedOutcomes map[string]string `json:"expected_outcomes,omitempty"`
}

type RetrievalFixture struct {
	Version          string             `json:"version"`
	Provenance       string             `json:"provenance"`
	AnnotationStatus string             `json:"annotation_status"`
	TopK             int                `json:"top_k"`
	InitialK         int                `json:"initial_k"`
	MaxK             int                `json:"max_k"`
	Products         []RetrievalProduct `json:"products"`
	Cases            []RetrievalCase    `json:"cases"`
}

// Keep parsed data private so a caller cannot change labels/ranks while retaining
// the SHA-256 of a different file. RunRetrieval never constructs remote clients.
type RetrievalDataset struct {
	fixture RetrievalFixture
	sha256  string
}

func LoadRetrieval(r io.Reader) (RetrievalDataset, error) {
	data, err := readBounded(r)
	if err != nil {
		return RetrievalDataset{}, err
	}
	var fixture RetrievalFixture
	if !utf8.Valid(data) || !uniqueRetrievalJSON(data) {
		return RetrievalDataset{}, errors.New("invalid or ambiguous retrieval JSON")
	}
	if err := decodeStrict(data, &fixture); err != nil {
		return RetrievalDataset{}, errors.New("invalid retrieval fixture JSON")
	}
	if err := fixture.validate(); err != nil {
		return RetrievalDataset{}, err
	}
	return RetrievalDataset{fixture: fixture, sha256: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

// Reject duplicate object keys (including escaped aliases), trailing documents
// and excessive nesting before binding a reproducible fixture to its digest.
func uniqueRetrievalJSON(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 16 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return true
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for d.More() {
				token, err := d.Token()
				key, ok := token.(string)
				// encoding/json also matches struct fields case-insensitively.
				key = strings.ToLower(key)
				if err != nil || !ok || seen[key] {
					return false
				}
				seen[key] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func (f RetrievalFixture) config() rag.Config {
	return rag.Config{TopK: f.TopK, Retrieval: rag.RetrievalConfig{Strategy: rag.StrategyHybridRRF,
		InitialK: f.InitialK, MaxK: f.MaxK, TimeoutMillis: 3000}}
}

func (f RetrievalFixture) validate() error {
	if !safeID.MatchString(f.Version) || f.Provenance != "synthetic_rankings" || f.AnnotationStatus != "pending_human_review" {
		return errors.New("retrieval fixture must declare synthetic, unreviewed rankings")
	}
	// Explicit bounds only: do not hash zero values and silently execute defaults.
	if f.TopK < 1 || f.InitialK < 1 || f.MaxK < 8 || f.config().Retrieval.Validate(f.TopK) != nil {
		return errors.New("invalid retrieval replay bounds (max-k must include legacy floor 8)")
	}
	if len(f.Products) < 1 || len(f.Products) > 256 || len(f.Cases) < 1 || len(f.Cases) > 256 {
		return errors.New("invalid retrieval fixture size")
	}
	products := make(map[string]RetrievalProduct, len(f.Products))
	for _, p := range f.Products {
		if !candidatecontract.ValidID(p.ID) || !candidatecontract.ValidID(p.ProductID) || products[p.ID].ID != "" ||
			strings.TrimSpace(p.Name) == "" || utf8.RuneCountInString(p.Name) > 256 || !utf8.ValidString(p.Name) ||
			p.PriceCents < 0 || p.PriceCents > agent.MaxBudgetCents || p.Stock < 0 || p.Sold < 0 {
			return errors.New("invalid or duplicate retrieval product")
		}
		products[p.ID] = p
	}
	seen := make(map[string]bool)
	for i, c := range f.Cases {
		bad := func(reason string) error { return fmt.Errorf("retrieval case %d: %s", i+1, reason) }
		if !safeID.MatchString(c.ID) || seen[c.ID] || (c.Kind != "quality" && c.Kind != "fault") {
			return bad("invalid case identifier or kind")
		}
		seen[c.ID] = true
		if _, err := agent.NewConstraints(agent.Intent{BudgetCents: c.Input.BudgetCents, MaxItems: c.Input.MaxItems}); err != nil ||
			strings.TrimSpace(c.Input.Query) == "" || strings.TrimSpace(c.Input.Query) != c.Input.Query ||
			!utf8.ValidString(c.Input.Query) || utf8.RuneCountInString(c.Input.Query) > agent.MaxQueryRunes {
			return bad("invalid resolved search input")
		}
		fault := false
		for _, lane := range []RetrievalLane{c.Keyword, c.Vector} {
			if lane.IDs == nil || len(lane.IDs) > f.MaxK || !validRetrievalFault(lane.Error) || !validRetrievalFault(lane.WiderError) ||
				(lane.Error != "" && lane.WiderError != "") || (lane.WiderError != "" && f.InitialK == f.MaxK) {
				return bad("invalid lane bounds or fault")
			}
			fault = fault || lane.Error != "" || lane.WiderError != ""
			for _, id := range lane.IDs {
				if products[id].ID == "" {
					return bad("ranking references unknown SKU")
				}
			}
		}
		labels := make(map[string]bool)
		if c.RelevantSKUs == nil {
			return bad("relevance labels must be explicit, including empty arrays")
		}
		for _, id := range c.RelevantSKUs {
			p, ok := products[id]
			if !ok || labels[id] || p.PriceCents <= 0 || p.PriceCents > c.Input.BudgetCents || p.Stock <= 0 {
				return bad("relevant SKU must be unique and eligible in the independent catalog")
			}
			labels[id] = true
		}
		if c.Kind == "quality" {
			if fault || len(c.ExpectedOutcomes) != 0 {
				return bad("quality cases cannot inject failures or override successful outcomes")
			}
		} else {
			if !fault || len(c.ExpectedOutcomes) != len(retrievalStrategies) {
				return bad("fault case requires a fault and an outcome for every strategy")
			}
			for _, strategy := range retrievalStrategies {
				code, ok := c.ExpectedOutcomes[strategy]
				if !ok || (code != "ok" && (code == "" || !validRetrievalFault(code))) {
					return bad("invalid expected strategy outcome")
				}
			}
		}
	}
	return nil
}

func validRetrievalFault(code string) bool {
	switch code {
	case "", "unavailable", "permission_denied", "unauthenticated", "canceled", "deadline_exceeded":
		return true
	}
	return false
}
