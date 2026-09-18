package tools

import (
	"sort"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
)

type rankedCandidate struct {
	candidate  ProductCandidate
	rank       int
	conflicted bool
}

// One lane can vote only once per SKU. Later facts invalidate earlier copies,
// but duplicates never improve the first appearance's rank.
func indexRanking(candidates []ProductCandidate) map[string]rankedCandidate {
	byID := make(map[string]rankedCandidate, len(candidates))
	for i, candidate := range candidates {
		rank := i + 1
		conflicted := false
		if old, exists := byID[candidate.Id]; exists {
			rank = old.rank
			conflicted = old.conflicted || old.candidate.Evidence.ProductID != candidate.Evidence.ProductID
		}
		byID[candidate.Id] = rankedCandidate{candidate: candidate, rank: rank, conflicted: conflicted}
	}
	return byID
}

// fuseRankings uses sum(1/(60+rank)), not a sum of incomparable raw scores.
// Keyword facts take deterministic precedence over vector snapshots; they are
// STILL unverified. Parent conflicts are excluded, not silently merged.
func fuseRankings(keyword, vector []ProductCandidate, budget int64, limit int) ([]ProductCandidate, int) {
	kw, vec := indexRanking(keyword), indexRanking(vector)
	ids := make(map[string]bool, len(kw)+len(vec))
	for id := range kw {
		ids[id] = true
	}
	for id := range vec {
		ids[id] = true
	}
	conflicts := 0
	out := make([]ProductCandidate, 0, len(ids))
	for id := range ids {
		k, hasKeyword := kw[id]
		v, hasVector := vec[id]
		if k.conflicted || v.conflicted || (hasKeyword && hasVector && k.candidate.Evidence.ProductID != v.candidate.Evidence.ProductID) {
			conflicts++
			continue
		}
		candidate := v.candidate
		if hasKeyword {
			candidate = k.candidate
		}
		// Filtering AFTER choosing facts prevents a stale cheap/stocked vector
		// from resurrecting a keyword snapshot which is invalid or sold out.
		if candidate.PriceCents <= 0 || candidate.PriceCents > budget || candidate.Stock <= 0 || candidate.Sold < 0 {
			continue
		}
		candidate.Tags = append([]string(nil), candidate.Tags...)
		candidate.Evidence.State = agentcore.VerificationUnverified
		candidate.Evidence.VerifiedAtUnixMs = 0
		ranking := agentcore.CandidateRanking{Method: "hybrid_rrf_v1"}
		if hasKeyword {
			ranking.KeywordRank = k.rank
			ranking.FusionScore += 1 / float64(rag.RRFConstant+k.rank)
		}
		if hasVector {
			ranking.VectorRank = v.rank
			ranking.FusionScore += 1 / float64(rag.RRFConstant+v.rank)
			candidate.Evidence.Relevance = v.candidate.Evidence.Relevance
			candidate.Evidence.HasRelevance = v.candidate.Evidence.HasRelevance
		}
		candidate.Evidence.Ranking = ranking
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Evidence.Ranking, out[j].Evidence.Ranking
		if a.FusionScore != b.FusionScore {
			return a.FusionScore > b.FusionScore
		}
		return out[i].Id < out[j].Id // byte-order tie-break independent of arrival/map order
	})
	return out[:min(len(out), limit)], conflicts
}
