package eval

import (
	"math"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

type RetrievalMean struct {
	Sum     float64  `json:"sum"`
	Samples int      `json:"samples"`
	Value   *float64 `json:"value"`
}

type RetrievalWork struct {
	KeywordCalls      int `json:"keyword_calls"`
	VectorCalls       int `json:"vector_calls"`
	Expansions        int `json:"expansions"`
	Fallbacks         int `json:"fallbacks"`
	DegradedSuccesses int `json:"degraded_successes"`
}

type RetrievalSummary struct {
	Cases                     int           `json:"cases"`
	QualityCases              int           `json:"quality_cases"`
	FaultCases                int           `json:"fault_cases"`
	OutcomeAccuracy           Ratio         `json:"outcome_accuracy"`
	ViolationCases            int           `json:"violation_cases"`
	RecallAtK                 Ratio         `json:"recall_at_k_micro"`
	PrecisionAtK              Ratio         `json:"precision_at_k"`
	NDCGAtK                   RetrievalMean `json:"ndcg_at_k_macro"`
	NoRelevantCases           int           `json:"no_relevant_cases"`
	NegativeFalsePositiveRate Ratio         `json:"negative_false_positive_rate"`
	AllWork                   RetrievalWork `json:"all_work"`
	QualityWork               RetrievalWork `json:"quality_work"`
	ReplayP50MS               float64       `json:"quality_replay_p50_ms"`
	ReplayP95MS               float64       `json:"quality_replay_p95_ms"`
	GatePassed                bool          `json:"gate_passed"`
}

// assessRetrieval evaluates a common downstream boundary: NormalizeCandidates,
// per-SKU affordability, then TopK. It does NOT evaluate bundle feasibility or
// live stock. Facts/labels come from the independent catalog, not result evidence.
func assessRetrieval(products map[string]RetrievalProduct, c RetrievalCase, candidates []agent.ProductCandidate, err error, k int) RetrievalOutcome {
	out := RetrievalOutcome{ID: c.ID, Kind: c.Kind, Outcome: retrievalOutcome(err), ReturnedIDs: []string{},
		RawCount: len(candidates), RelevantTotal: len(c.RelevantSKUs), Violations: []string{}, Rankings: []agent.CandidateRanking{}}
	if err != nil {
		if len(candidates) != 0 {
			out.Violations = append(out.Violations, "candidates_on_error")
		}
		candidates = nil
	}
	eligible := replayEligible(candidates, c.Input.BudgetCents)
	eligible = eligible[:min(k, len(eligible))]
	seen := make(map[string]bool)
	for _, candidate := range eligible {
		p, ok := products[candidate.Id]
		if !ok || seen[candidate.Id] || p.PriceCents <= 0 || p.PriceCents > c.Input.BudgetCents || p.Stock <= 0 ||
			candidate.Evidence.ProductID != p.ProductID || candidate.Name != p.Name || candidate.PriceCents != p.PriceCents ||
			candidate.Stock != p.Stock || candidate.Sold != p.Sold ||
			(candidate.Evidence.Source != agent.RetrievalMallKeyword && candidate.Evidence.Source != agent.RetrievalMallVector) {
			out.Violations = append(out.Violations, "catalog_fact_mismatch")
		}
		seen[candidate.Id] = true
		out.ReturnedIDs = append(out.ReturnedIDs, candidate.Id)
		out.Rankings = append(out.Rankings, candidate.Evidence.Ranking)
	}
	out.RelevantHits, out.NDCG = retrievalQuality(out.ReturnedIDs, c.RelevantSKUs, k)
	zeroUnsafeRetrievalQuality(&out)
	return out
}

// Binary relevance: DCG=sum(hit/log2(rank+1)); ideal ranking contains
// min(K, relevant count) hits. No relevant labels => NDCG undefined, not 1.
func retrievalQuality(ids, relevant []string, k int) (int, *float64) {
	labels, seen := make(map[string]bool), make(map[string]bool)
	for _, id := range relevant {
		labels[id] = true
	}
	if len(labels) == 0 {
		return 0, nil
	}
	hits, dcg, ideal := 0, 0.0, 0.0
	for i, id := range ids[:min(k, len(ids))] {
		if labels[id] && !seen[id] {
			hits++
			dcg += 1 / math.Log2(float64(i+2))
		}
		seen[id] = true
	}
	for i := 0; i < min(k, len(labels)); i++ {
		ideal += 1 / math.Log2(float64(i+2))
	}
	value := dcg / ideal
	return hits, &value
}

func zeroUnsafeRetrievalQuality(out *RetrievalOutcome) {
	if len(out.Violations) == 0 {
		return
	}
	out.RelevantHits = 0
	if out.NDCG != nil {
		*out.NDCG = 0
	}
}

func (w *RetrievalWork) add(c RetrievalOutcome) {
	for _, read := range c.Reads {
		if read.Lane == "keyword" {
			w.KeywordCalls++
		} else {
			w.VectorCalls++
		}
	}
	if c.Expanded {
		w.Expansions++
	}
	if c.Fallback {
		w.Fallbacks++
	}
	if c.Degraded {
		w.DegradedSuccesses++
	}
}

func summarizeRetrieval(cases []RetrievalOutcome, k int) RetrievalSummary {
	s := RetrievalSummary{Cases: len(cases), GatePassed: len(cases) > 0}
	matched, hits, relevant, falsePositives := 0, 0, 0, 0
	var latencies []float64
	for _, c := range cases {
		if c.OutcomeMatched {
			matched++
		} else {
			s.GatePassed = false
		}
		if len(c.Violations) > 0 {
			s.ViolationCases++
			s.GatePassed = false
		}
		s.AllWork.add(c)
		if c.Kind != "quality" {
			s.FaultCases++
			continue
		}
		s.QualityCases++
		s.QualityWork.add(c)
		latencies = append(latencies, c.ReplayMS)
		hits += c.RelevantHits
		relevant += c.RelevantTotal
		if c.NDCG != nil {
			s.NDCGAtK.Sum += *c.NDCG
			s.NDCGAtK.Samples++
		}
		if c.RelevantTotal == 0 {
			s.NoRelevantCases++
			if len(c.ReturnedIDs) > 0 {
				falsePositives++
			}
		}
	}
	s.OutcomeAccuracy = ratio(matched, len(cases))
	s.RecallAtK = ratio(hits, relevant)
	// Missing ranks count as misses: do not inflate precision by returning fewer.
	s.PrecisionAtK = ratio(hits, k*s.QualityCases)
	s.NegativeFalsePositiveRate = ratio(falsePositives, s.NoRelevantCases)
	if s.NDCGAtK.Samples > 0 {
		value := s.NDCGAtK.Sum / float64(s.NDCGAtK.Samples)
		s.NDCGAtK.Value = &value
	}
	s.ReplayP50MS, s.ReplayP95MS = percentile(latencies, .5), percentile(latencies, .95)
	return s
}
