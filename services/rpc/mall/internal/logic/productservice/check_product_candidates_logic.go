package productservicelogic

import (
	"context"
	"time"

	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type CheckProductCandidatesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCheckProductCandidatesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckProductCandidatesLogic {
	return &CheckProductCandidatesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CheckProductCandidatesLogic) CheckProductCandidates(in *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
	if in == nil || !candidatecontract.ValidIDs(in.SkuIds) {
		return nil, status.Error(codes.InvalidArgument, "invalid candidate check bounds")
	}
	if l.svcCtx == nil || l.svcCtx.CandidateStore == nil {
		return nil, status.Error(codes.Unavailable, "candidate store unavailable")
	}
	ctx, cancel := context.WithTimeout(l.ctx, candidatecontract.MaxDuration)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	checkedAt := time.Now().UnixMilli()
	read := l.svcCtx.CandidateStore.FindActiveCandidates
	if in.IncludeDemandCategory {
		store, ok := l.svcCtx.CandidateStore.(product_index.DemandCandidateReader)
		if !ok {
			return nil, status.Error(codes.Unavailable, "demand classification unavailable")
		}
		read = store.FindActiveDemandCandidates
	}
	rows, err := read(ctx, in.SkuIds)
	if stopped := ctx.Err(); stopped != nil {
		return nil, status.FromContextError(stopped).Err()
	}
	if err != nil {
		l.Logger.Error("candidate check read failed")
		return nil, status.Error(codes.Unavailable, "candidate store unavailable")
	}
	if len(rows) > len(in.SkuIds) {
		return nil, status.Error(codes.Unavailable, "invalid candidate store response")
	}
	requested := make(map[string]bool, len(in.SkuIds))
	for _, id := range in.SkuIds {
		requested[id] = true
	}
	byID := make(map[string]product_index.CandidateFacts, len(rows))
	for _, row := range rows {
		_, duplicate := byID[row.SkuId]
		if !requested[row.SkuId] || duplicate || !candidatecontract.ValidID(row.ProductId) {
			return nil, status.Error(codes.Unavailable, "invalid candidate store response")
		}
		if in.IncludeDemandCategory && !candidatecontract.ValidDemandCategoryFact(row.CategoryCode, row.CategoryTaxonomy, row.CategoryRevision) {
			return nil, status.Error(codes.Unavailable, "invalid demand classification")
		}
		byID[row.SkuId] = row
	}
	resp := &pb.CheckProductCandidatesResp{CheckedAtUnixMs: checkedAt}
	if in.IncludeDemandCategory {
		resp.DemandCategoryContract = candidatecontract.DemandCategoryContract
	}
	for _, id := range in.SkuIds {
		check := &pb.CandidateCheck{SkuId: id, State: pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE}
		if row, ok := byID[id]; ok {
			check.State = pb.CandidateState_CANDIDATE_STATE_ACTIVE
			check.Facts = &pb.CandidateFacts{SkuId: id, ProductId: row.ProductId,
				ProductName: row.ProductName, SkuName: row.SkuName, Price: row.Price, Stock: row.Stock, Sold: row.Sold}
			if in.IncludeDemandCategory {
				check.Facts.DemandCategory = &pb.DemandCategoryFact{Code: row.CategoryCode,
					TaxonomyVersion: row.CategoryTaxonomy, Revision: row.CategoryRevision}
			}
		}
		resp.Results = append(resp.Results, check)
	}
	if proto.Size(resp) > candidatecontract.MaxResponseBytes {
		return nil, status.Error(codes.ResourceExhausted, "candidate check response exceeds limit")
	}
	return resp, nil
}
