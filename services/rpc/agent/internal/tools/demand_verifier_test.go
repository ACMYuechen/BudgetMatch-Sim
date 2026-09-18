package tools

import (
	"context"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func classifiedResponse(ids ...string) *pb.CheckProductCandidatesResp {
	r := checkResponse(ids...)
	r.DemandCategoryContract = candidatecontract.DemandCategoryContract
	for _, item := range r.Results {
		item.Facts.DemandCategory = &pb.DemandCategoryFact{Code: "keyboard", TaxonomyVersion: candidatecontract.DemandTaxonomyVersion, Revision: 3}
	}
	return r
}

func TestDemandVerifierRequiresVersionAndPerSKUCategory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*pb.CheckProductCandidatesResp)
	}{
		{"old server", func(r *pb.CheckProductCandidatesResp) { r.DemandCategoryContract = "" }},
		{"other version", func(r *pb.CheckProductCandidatesResp) { r.DemandCategoryContract = "v2" }},
		{"missing fact", func(r *pb.CheckProductCandidatesResp) { r.Results[0].Facts.DemandCategory = nil }},
		{"demo", func(r *pb.CheckProductCandidatesResp) {
			r.Results[0].Facts.DemandCategory.TaxonomyVersion = "demo_taxonomy_v1"
		}},
		{"invented category", func(r *pb.CheckProductCandidatesResp) { r.Results[0].Facts.DemandCategory.Code = "quiet" }},
		{"invalid revision", func(r *pb.CheckProductCandidatesResp) { r.Results[0].Facts.DemandCategory.Revision = 0 }},
		{"unavailable cannot carry facts", func(r *pb.CheckProductCandidatesResp) {
			r.Results[0].State = pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := candidateClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
				require.True(t, req.IncludeDemandCategory)
				r := classifiedResponse("a")
				tc.change(r)
				return r, nil
			})
			batch, err := NewMallDemandVerifier(client).Verify(context.Background(), []ProductCandidate{staleCandidate("a")})
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.Empty(t, batch.Candidates)
		})
	}
}

func TestDemandVerifierFreshEvidenceAndLegacyClearing(t *testing.T) {
	input := staleCandidate("a")
	input.Evidence.DemandCategory = agent.DemandCategoryEvidence{Code: "mouse", TaxonomyVersion: "forged", Revision: 99}
	for _, demandMode := range []bool{false, true} {
		client := candidateClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
			require.Equal(t, demandMode, req.IncludeDemandCategory)
			r := classifiedResponse("a", "b", "c")
			r.Results[0].Facts.DemandCategory.Code = "unknown"
			r.Results[0].Facts.DemandCategory.Revision = 0
			r.Results[1] = &pb.CandidateCheck{SkuId: "b", State: pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE}
			r.Results[2].Facts.Stock = 0
			return r, nil
		})
		v := NewMallCandidateVerifier(client)
		if demandMode {
			v = NewMallDemandVerifier(client)
		}
		batch, err := v.Verify(context.Background(), []ProductCandidate{input, staleCandidate("b"), staleCandidate("c")})
		require.NoError(t, err)
		require.Len(t, batch.Candidates, 1)
		require.Equal(t, []string{"b", "c"}, batch.Unavailable)
		if demandMode {
			require.Equal(t, "unknown", batch.Candidates[0].Evidence.DemandCategory.Code)
			require.Equal(t, candidatecontract.DemandTaxonomyVersion, batch.Candidates[0].Evidence.DemandCategory.TaxonomyVersion)
		} else {
			require.Equal(t, agent.DemandCategoryEvidence{}, batch.Candidates[0].Evidence.DemandCategory)
		}
	}
	require.Equal(t, "forged", input.Evidence.DemandCategory.TaxonomyVersion)
}
