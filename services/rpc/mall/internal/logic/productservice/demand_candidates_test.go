package productservicelogic

import (
	"context"
	"errors"
	"testing"

	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type classifiedReader struct {
	candidateReaderFunc
	demand candidateReaderFunc
}

func (r classifiedReader) FindActiveDemandCandidates(ctx context.Context, ids []string) ([]product_index.CandidateFacts, error) {
	return r.demand(ctx, ids)
}

func TestDemandCheckRequiresOptInAndReturnsExplicitUnknown(t *testing.T) {
	legacyCalls, demandCalls := 0, 0
	row := product_index.CandidateFacts{SkuId: "a", ProductId: "p", Price: 100, Stock: 1, CategoryCode: "keyboard", CategoryTaxonomy: candidatecontract.DemandTaxonomyVersion, CategoryRevision: 3}
	store := classifiedReader{candidateReaderFunc: func(context.Context, []string) ([]product_index.CandidateFacts, error) {
		legacyCalls++
		return []product_index.CandidateFacts{row}, nil
	},
		demand: func(context.Context, []string) ([]product_index.CandidateFacts, error) {
			demandCalls++
			return []product_index.CandidateFacts{row}, nil
		}}
	logic := NewCheckProductCandidatesLogic(context.Background(), &svc.ServiceContext{CandidateStore: store})
	resp, err := logic.CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a"}})
	require.NoError(t, err)
	require.Empty(t, resp.DemandCategoryContract)
	require.Nil(t, resp.Results[0].Facts.DemandCategory)
	require.Equal(t, 1, legacyCalls)
	require.Zero(t, demandCalls)
	for _, code := range []string{"keyboard", "unknown"} {
		row.CategoryCode = code
		if code == "unknown" {
			row.CategoryRevision = 0
		}
		resp, err = logic.CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a", "absent"}, IncludeDemandCategory: true})
		require.NoError(t, err)
		require.Equal(t, candidatecontract.DemandCategoryContract, resp.DemandCategoryContract)
		require.Equal(t, code, resp.Results[0].Facts.DemandCategory.Code)
		require.Equal(t, row.CategoryRevision, resp.Results[0].Facts.DemandCategory.Revision)
		require.Equal(t, pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE, resp.Results[1].State)
		require.Nil(t, resp.Results[1].Facts)
	}
	require.Equal(t, 1, legacyCalls)
	require.Equal(t, 2, demandCalls)
}

func TestDemandCheckNeverFallsBackOnMissingSchemaOrInvalidClassification(t *testing.T) {
	for _, tc := range []struct {
		name, code, taxonomy string
		revision             int64
		err                  error
	}{
		{"schema absent", "keyboard", candidatecontract.DemandTaxonomyVersion, 1, errors.New("secret DSN table missing")},
		{"implicit unknown", "", "", 0, nil}, {"demo label", "keyboard", "demo_taxonomy_v1", 1, nil},
		{"attribute", "quiet", candidatecontract.DemandTaxonomyVersion, 1, nil}, {"bad revision", "keyboard", candidatecontract.DemandTaxonomyVersion, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := classifiedReader{candidateReaderFunc: func(context.Context, []string) ([]product_index.CandidateFacts, error) {
				t.Fatal("legacy fallback")
				return nil, nil
			},
				demand: func(context.Context, []string) ([]product_index.CandidateFacts, error) {
					return []product_index.CandidateFacts{{SkuId: "a", ProductId: "p", CategoryCode: tc.code, CategoryTaxonomy: tc.taxonomy, CategoryRevision: tc.revision}}, tc.err
				}}
			resp, err := NewCheckProductCandidatesLogic(context.Background(), &svc.ServiceContext{CandidateStore: store}).CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a"}, IncludeDemandCategory: true})
			require.Nil(t, resp)
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.NotContains(t, err.Error(), "secret")
		})
	}
	old := candidateReaderFunc(func(context.Context, []string) ([]product_index.CandidateFacts, error) {
		t.Fatal("legacy fallback")
		return nil, nil
	})
	_, err := NewCheckProductCandidatesLogic(context.Background(), &svc.ServiceContext{CandidateStore: old}).CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a"}, IncludeDemandCategory: true})
	require.Equal(t, codes.Unavailable, status.Code(err))
}
