package productindexservicelogic

import (
	"context"
	"errors"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/indexcontract"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type ScanProductIndexLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewScanProductIndexLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ScanProductIndexLogic {
	return &ScanProductIndexLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// One bounded, read-only database snapshot. No resume/fallback to live pages.
func (l *ScanProductIndexLogic) ScanProductIndex(in *pb.ScanProductIndexReq, stream pb.ProductIndexService_ScanProductIndexServer) error {
	// Use the actual transport context: a timeout on l.ctx alone cannot bound Send.
	ctx := stream.Context()
	if ctx.Value(interceptor.ContextKeyServiceName) != serviceauth.ServiceAgent ||
		ctx.Value(interceptor.ContextKeyServicePurpose) != serviceauth.PurposeProductIndexRead {
		return apperrors.Unauthorized
	}
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if in == nil || in.PageSize < 0 || in.PageSize > indexcontract.MaxPageSize {
		return apperrors.Invalid
	}
	if err := product_index.ValidateSnapshotContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return status.FromContextError(err).Err()
		}
		return status.Error(codes.InvalidArgument, "snapshot RPC requires a deadline within 30 seconds")
	}
	if l.svcCtx.ProductIndexSnapshots == nil {
		return apperrors.Internal
	}
	size := int(in.PageSize)
	if size == 0 {
		size = indexcontract.DefaultPageSize
	}
	id, sequence := uuid.NewString(), uint32(0)
	var budget indexcontract.Budget
	var sendErr error
	send := func(frame *pb.ScanProductIndexResp) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sequence++
		frame.SnapshotId, frame.Sequence = id, sequence
		if !budget.Add(len(frame.List), proto.Size(frame)) {
			return product_index.ErrSnapshotLimit
		}
		sendErr = stream.Send(frame)
		return sendErr
	}
	err := l.svcCtx.ProductIndexSnapshots.ScanSnapshot(ctx, size, func(rows []product_index.Entry) error {
		if len(rows) == 0 || len(rows) > size {
			return errors.New("invalid snapshot page")
		}
		frame := &pb.ScanProductIndexResp{LastSkuId: rows[len(rows)-1].SkuId}
		for _, row := range rows {
			frame.List = append(frame.List, &pb.ProductIndexEntry{
				SkuId: row.SkuId, ProductId: row.ProductId, ProductName: row.ProductName,
				ProductContent: row.ProductContent, Provider: row.Provider,
				SkuName: row.SkuName, Specs: row.Specs, Price: row.Price, Stock: row.Stock, Sold: row.Sold,
			})
		}
		return send(frame)
	})
	if err == nil {
		// No success marker until the read transaction has committed. The client
		// additionally requires clean EOF in case delivery/trailers fail afterward.
		err = send(&pb.ScanProductIndexResp{Complete: true, TotalItems: uint64(budget.Items)})
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	if errors.Is(err, product_index.ErrSnapshotBusy) {
		return status.Error(codes.ResourceExhausted, "product index snapshot busy")
	}
	if errors.Is(err, product_index.ErrSnapshotLimit) {
		return status.Error(codes.ResourceExhausted, "product index snapshot limit exceeded")
	}
	if sendErr != nil {
		return status.Error(codes.Unavailable, "product index snapshot delivery failed")
	}
	// No raw database errors, tokens or product contents in logs/trailers.
	l.Logger.Error("product index snapshot read failed")
	return apperrors.Database
}
