package tools

import (
	"context"
	"math"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type CandidateCheckClient interface {
	CheckProductCandidates(context.Context, *pb.CheckProductCandidatesReq, ...grpc.CallOption) (*pb.CheckProductCandidatesResp, error)
}

type CandidateBatch struct {
	Candidates      []ProductCandidate
	CheckedAtUnixMs int64
}

type CandidateVerifier interface {
	Verify(context.Context, []ProductCandidate) (CandidateBatch, error)
}

// MallCandidateVerifier makes one bounded RPC using the online user client.
// Errors/partial replies never become successful stale-snapshot recommendations.
type MallCandidateVerifier struct{ client CandidateCheckClient }

func NewMallCandidateVerifier(client CandidateCheckClient) *MallCandidateVerifier {
	return &MallCandidateVerifier{client: client}
}

func candidateDependencyError() error {
	return status.Error(codes.Unavailable, "candidate verification unavailable")
}

func (v *MallCandidateVerifier) Verify(ctx context.Context, candidates []ProductCandidate) (CandidateBatch, error) {
	if err := ctx.Err(); err != nil {
		return CandidateBatch{}, err
	}
	if v == nil || v.client == nil {
		return CandidateBatch{}, candidateDependencyError()
	}
	ids := make([]string, 0, len(candidates))
	byID := make(map[string]ProductCandidate, len(candidates))
	for _, candidate := range candidates {
		e := candidate.Evidence
		if (e.Source != agentcore.RetrievalMallKeyword && e.Source != agentcore.RetrievalMallVector) ||
			!candidatecontract.ValidID(e.ProductID) || e.State == agentcore.VerificationDemo ||
			(e.HasRelevance && (math.IsNaN(e.Relevance) || math.IsInf(e.Relevance, 0))) {
			return CandidateBatch{}, candidateDependencyError()
		}
		ids = append(ids, candidate.Id)
		byID[candidate.Id] = candidate
	}
	if !candidatecontract.ValidIDs(ids) {
		return CandidateBatch{}, candidateDependencyError()
	}
	checkCtx, cancel := context.WithTimeout(ctx, candidatecontract.MaxDuration)
	defer cancel()
	started := time.Now()
	resp, err := v.client.CheckProductCandidates(checkCtx, &pb.CheckProductCandidatesReq{SkuIds: ids},
		grpc.MaxCallRecvMsgSize(candidatecontract.MaxResponseBytes))
	if stopped := ctx.Err(); stopped != nil {
		return CandidateBatch{}, stopped
	}
	if err != nil {
		switch status.Code(err) {
		case codes.Unauthenticated, codes.PermissionDenied, codes.Canceled:
			return CandidateBatch{}, safety.Protect(err)
		}
		return CandidateBatch{}, candidateDependencyError()
	}
	if checkCtx.Err() != nil || resp == nil || len(resp.Results) != len(ids) ||
		proto.Size(resp) > candidatecontract.MaxResponseBytes {
		return CandidateBatch{}, candidateDependencyError()
	}
	// Allow small host clock skew, but never accept obviously stale/future checks.
	const clockSkew = 30 * time.Second
	if resp.CheckedAtUnixMs < started.Add(-clockSkew).UnixMilli() ||
		resp.CheckedAtUnixMs > time.Now().Add(clockSkew).UnixMilli() {
		return CandidateBatch{}, candidateDependencyError()
	}
	checked := make(map[string]ProductCandidate, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, item := range resp.Results {
		if item == nil || seen[item.SkuId] {
			return CandidateBatch{}, candidateDependencyError()
		}
		candidate, exists := byID[item.SkuId]
		if !exists {
			return CandidateBatch{}, candidateDependencyError()
		}
		seen[item.SkuId] = true
		switch item.State {
		case pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE:
			if item.Facts != nil {
				return CandidateBatch{}, candidateDependencyError()
			}
			continue
		case pb.CandidateState_CANDIDATE_STATE_ACTIVE:
			facts := item.Facts
			if facts == nil || facts.SkuId != item.SkuId || facts.ProductId != candidate.Evidence.ProductID {
				return CandidateBatch{}, candidateDependencyError()
			}
			if facts.Price <= 0 || facts.Price > agentcore.MaxBudgetCents || facts.Stock <= 0 || facts.Sold < 0 {
				continue // confirmed but not a valid purchasable candidate
			}
			candidate.Name = joinName(facts.ProductName, facts.SkuName)
			candidate.PriceCents, candidate.Stock, candidate.Sold = facts.Price, facts.Stock, facts.Sold
			// Mall has no authoritative category yet; do not certify an old vector label.
			candidate.Category = ""
			candidate.Source = "mall"
			if candidate.Evidence.Source == agentcore.RetrievalMallVector {
				candidate.Source = "mall+rag"
			}
			candidate.Evidence.State = agentcore.VerificationChecked
			candidate.Evidence.VerifiedAtUnixMs = resp.CheckedAtUnixMs
			checked[item.SkuId] = candidate
		default:
			return CandidateBatch{}, candidateDependencyError()
		}
	}
	batch := CandidateBatch{CheckedAtUnixMs: resp.CheckedAtUnixMs}
	// Stable order despite server response reordering, including tie-breaking.
	for _, id := range ids {
		if candidate, ok := checked[id]; ok {
			batch.Candidates = append(batch.Candidates, candidate)
		}
	}
	return batch, nil
}
