package productindexservicelogic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/mall/indexcontract"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type snapshotReaderFunc func(context.Context, int, func([]product_index.Entry) error) error

func (f snapshotReaderFunc) ScanSnapshot(ctx context.Context, size int, visit func([]product_index.Entry) error) error {
	return f(ctx, size, visit)
}

type snapshotServer struct {
	grpc.ServerStream
	ctx    context.Context
	frames []*pb.ScanProductIndexResp
	send   func(*pb.ScanProductIndexResp) error
}

func (s *snapshotServer) Context() context.Context { return s.ctx }
func (s *snapshotServer) Send(frame *pb.ScanProductIndexResp) error {
	if s.send != nil {
		if err := s.send(frame); err != nil {
			return err
		}
	}
	s.frames = append(s.frames, frame)
	return nil
}
func snapshotContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(indexContext(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestSnapshotLogicCommitsBeforeTerminal(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			stream := &snapshotServer{ctx: snapshotContext(t)}
			committed := false
			stream.send = func(f *pb.ScanProductIndexResp) error { require.Equal(t, committed, f.Complete); return nil }
			store := snapshotReaderFunc(func(ctx context.Context, size int, visit func([]product_index.Entry) error) error {
				require.Equal(t, 1, size)
				require.Equal(t, stream.ctx, ctx)
				for i := range count {
					require.NoError(t, visit([]product_index.Entry{{SkuId: string(rune('a' + i)), ProductId: "p1", ProductName: "name", ProductContent: "detail", Provider: "brand", SkuName: "sku", Specs: "{}", Price: 5, Stock: 6, Sold: 7}}))
				}
				committed = true
				return nil
			})
			err := NewScanProductIndexLogic(stream.ctx, &svc.ServiceContext{ProductIndexSnapshots: store}).ScanProductIndex(&pb.ScanProductIndexReq{PageSize: 1}, stream)
			require.NoError(t, err)
			require.Len(t, stream.frames, count+1)
			for i, f := range stream.frames {
				require.NoError(t, uuid.Validate(f.SnapshotId))
				require.Equal(t, stream.frames[0].SnapshotId, f.SnapshotId)
				require.EqualValues(t, i+1, f.Sequence)
				if i < count {
					require.False(t, f.Complete)
					require.Equal(t, f.List[0].SkuId, f.LastSkuId)
					require.True(t, proto.Equal(&pb.ProductIndexEntry{SkuId: string(rune('a' + i)), ProductId: "p1", ProductName: "name", ProductContent: "detail", Provider: "brand", SkuName: "sku", Specs: "{}", Price: 5, Stock: 6, Sold: 7}, f.List[0]))
				} else {
					require.True(t, f.Complete)
					require.Empty(t, f.List)
					require.Empty(t, f.LastSkuId)
					require.EqualValues(t, count, f.TotalItems)
				}
			}
		})
	}
}

func TestSnapshotLogicAdmissionBeforeRead(t *testing.T) {
	longCtx, cancel := context.WithTimeout(indexContext(), time.Minute)
	defer cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		req  *pb.ScanProductIndexReq
		code codes.Code
	}{
		{"no identity", context.Background(), &pb.ScanProductIndexReq{}, codes.Unauthenticated},
		{"wrong purpose", context.WithValue(snapshotContext(t), interceptor.ContextKeyServicePurpose, "write"), &pb.ScanProductIndexReq{}, codes.Unauthenticated},
		{"no deadline", indexContext(), &pb.ScanProductIndexReq{}, codes.InvalidArgument},
		{"long deadline", longCtx, &pb.ScanProductIndexReq{}, codes.InvalidArgument},
		{"nil request", snapshotContext(t), nil, codes.InvalidArgument},
		{"negative size", snapshotContext(t), &pb.ScanProductIndexReq{PageSize: -1}, codes.InvalidArgument},
		{"large size", snapshotContext(t), &pb.ScanProductIndexReq{PageSize: 201}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &snapshotServer{ctx: tc.ctx}
			store := snapshotReaderFunc(func(context.Context, int, func([]product_index.Entry) error) error {
				t.Fatal("read before admission")
				return nil
			})
			// A derived logic context must not disguise an unbounded transport.
			err := NewScanProductIndexLogic(snapshotContext(t), &svc.ServiceContext{ProductIndexSnapshots: store}).ScanProductIndex(tc.req, stream)
			require.Equal(t, tc.code, status.Code(err))
			require.Empty(t, stream.frames)
		})
	}
	err := NewScanProductIndexLogic(snapshotContext(t), &svc.ServiceContext{}).ScanProductIndex(&pb.ScanProductIndexReq{}, &snapshotServer{ctx: snapshotContext(t)})
	require.ErrorIs(t, err, apperrors.Internal)
}

func TestSnapshotLogicFailuresNeverEmitCompletion(t *testing.T) {
	for _, mode := range []string{"database", "commit", "send", "busy", "page limit", "item limit", "cancel", "cancel after commit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(snapshotContext(t))
			defer cancel()
			stream := &snapshotServer{ctx: ctx}
			if mode == "send" {
				stream.send = func(*pb.ScanProductIndexResp) error { return errors.New("private transport info") }
			}
			store := snapshotReaderFunc(func(ctx context.Context, size int, visit func([]product_index.Entry) error) error {
				require.Equal(t, indexcontract.DefaultPageSize, size)
				if mode == "busy" {
					return product_index.ErrSnapshotBusy
				}
				if mode == "database" {
					return errors.New("private database info")
				}
				if mode == "cancel" {
					cancel()
				}
				row := product_index.Entry{SkuId: "s1", ProductId: "p1"}
				if mode == "page limit" {
					row.Specs = strings.Repeat("x", indexcontract.MaxPageBytes)
				}
				if err := visit([]product_index.Entry{row}); err != nil {
					return err
				}
				if mode == "cancel after commit" {
					cancel()
					return nil
				}
				if mode == "item limit" {
					return product_index.ErrSnapshotLimit
				}
				return errors.New("private commit info")
			})
			err := NewScanProductIndexLogic(ctx, &svc.ServiceContext{ProductIndexSnapshots: store}).ScanProductIndex(&pb.ScanProductIndexReq{}, stream)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			for _, f := range stream.frames {
				require.False(t, f.Complete)
			}
			switch mode {
			case "busy", "page limit", "item limit":
				require.Equal(t, codes.ResourceExhausted, status.Code(err))
			case "cancel", "cancel after commit":
				require.Equal(t, codes.Canceled, status.Code(err))
			case "send":
				require.Equal(t, codes.Unavailable, status.Code(err))
			default:
				require.ErrorIs(t, err, apperrors.Database)
			}
		})
	}
}
