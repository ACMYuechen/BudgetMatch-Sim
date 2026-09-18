package rag

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/mall/indexcontract"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/cloudwego/eino/components/document"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testSnapshotID = "134fa4b4-d5f8-4c70-ad8b-2366e976121a"

type snapshotCatalogFunc func(context.Context, *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error)

func (f snapshotCatalogFunc) ScanProductIndex(ctx context.Context, req *pb.ScanProductIndexReq, _ ...grpc.CallOption) (pb.ProductIndexService_ScanProductIndexClient, error) {
	return f(ctx, req)
}

type snapshotReply struct {
	frame *pb.ScanProductIndexResp
	err   error
}
type snapshotClient struct {
	grpc.ClientStream
	replies    []snapshotReply
	beforeRecv func()
	calls      int
}

func (s *snapshotClient) Recv() (*pb.ScanProductIndexResp, error) {
	s.calls++
	if s.beforeRecv != nil {
		s.beforeRecv()
	}
	if len(s.replies) == 0 {
		return nil, io.EOF
	}
	reply := s.replies[0]
	s.replies = s.replies[1:]
	return reply.frame, reply.err
}
func catalogReplies(replies ...snapshotReply) snapshotCatalogFunc {
	return func(context.Context, *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
		return &snapshotClient{replies: append([]snapshotReply(nil), replies...)}, nil
	}
}
func dataFrame(seq uint32, ids ...string) *pb.ScanProductIndexResp {
	frame := &pb.ScanProductIndexResp{SnapshotId: testSnapshotID, Sequence: seq}
	for _, id := range ids {
		frame.List = append(frame.List, indexEntry(id))
	}
	if len(ids) > 0 {
		frame.LastSkuId = ids[len(ids)-1]
	}
	return frame
}
func completeFrame(seq uint32, total uint64) *pb.ScanProductIndexResp {
	return &pb.ScanProductIndexResp{SnapshotId: testSnapshotID, Sequence: seq, Complete: true, TotalItems: total}
}
func indexEntry(id string) *pb.ProductIndexEntry {
	return &pb.ProductIndexEntry{SkuId: id, ProductId: "p1", ProductName: "键盘", SkuName: "红轴",
		Provider: "K", ProductContent: "适合办公", Specs: "{\"switch\":\"red\"}", Price: 29900, Stock: 10, Sold: 5}
}

func TestLoaderLoadsOneSnapshot(t *testing.T) {
	stream := &snapshotClient{replies: []snapshotReply{
		{frame: dataFrame(1, "s1", "s2")}, {frame: dataFrame(2, "s3")}, {frame: completeFrame(3, 3)},
	}}
	calls := 0
	var rpcCtx context.Context
	catalog := snapshotCatalogFunc(func(ctx context.Context, in *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
		calls++
		rpcCtx = ctx
		require.EqualValues(t, 2, in.PageSize)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), indexcontract.MaxDuration)
		return stream, nil
	})
	scan, err := NewMallProductLoader(catalog, 2).LoadCatalog(context.Background())
	require.NoError(t, err)
	require.True(t, scan.Complete)
	require.Len(t, scan.Documents, 3)
	require.Equal(t, 1, calls)
	require.Equal(t, 4, stream.calls, "must read clean EOF after terminal marker")
	require.ErrorIs(t, rpcCtx.Err(), context.Canceled, "stream resources released on success")
	first := scan.Documents[0]
	require.Equal(t, "s1", first.ID)
	for _, s := range []string{"键盘 红轴", "供应商: K", "简介: 适合办公", "{\"switch\":\"red\"}"} {
		require.Contains(t, first.Content, s)
	}
	require.NotContains(t, first.Content, "29900")
	meta, ok := CandidateFromDocument(first)
	require.True(t, ok)
	require.Equal(t, "p1", meta.ProductId)
	require.EqualValues(t, 29900, meta.PriceCents)
	require.EqualValues(t, 10, meta.Stock)
	require.Equal(t, "mall", meta.Source)
	changed := buildSkuDocument(&pb.Product{Id: "p1", Name: "键盘", Providor: "K", Content: "适合办公"},
		&pb.Sku{Id: "s1", Name: "红轴", Specs: "{\"switch\":\"red\"}", Price: 59900, Stock: 2, Sold: 100})
	require.Equal(t, first.Content, changed.Content, "volatile metadata must not change embedding input")
}

func TestLoaderRejectsInvalidSnapshotAndPreservesOldIndex(t *testing.T) {
	mutate := func(f *pb.ScanProductIndexResp, change func(*pb.ScanProductIndexResp)) *pb.ScanProductIndexResp {
		change(f)
		return f
	}
	cases := []struct {
		name    string
		replies []snapshotReply
	}{
		{"premature EOF", nil},
		{"unavailable", []snapshotReply{{err: status.Error(codes.Unavailable, "test failure")}}},
		{"nil frame", []snapshotReply{{}}},
		{"empty data", []snapshotReply{{frame: dataFrame(2)}}},
		{"missing identity", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) { f.SnapshotId = "" })}}},
		{"different snapshot", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) { f.SnapshotId = "c9d7011a-6ee5-4cde-98e4-c94958e006cc" })}}},
		{"missing sequence", []snapshotReply{{frame: dataFrame(0, "s3")}}},
		{"skipped sequence", []snapshotReply{{frame: dataFrame(3, "s3")}}},
		{"repeated sequence", []snapshotReply{{frame: dataFrame(1, "s3")}}},
		{"nil entry", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) { f.List[0] = nil })}}},
		{"missing SKU", []snapshotReply{{frame: dataFrame(2, "")}}},
		{"invalid SKU", []snapshotReply{{frame: dataFrame(2, " s3")}}},
		{"long SKU", []snapshotReply{{frame: dataFrame(2, strings.Repeat("z", 65))}}},
		{"missing product", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) { f.List[0].ProductId = "" })}}},
		{"invalid product", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) { f.List[0].ProductId = " p1" })}}},
		{"duplicate across pages", []snapshotReply{{frame: dataFrame(2, "s2")}}},
		{"duplicate within page", []snapshotReply{{frame: dataFrame(2, "s3", "s3")}}},
		{"unordered", []snapshotReply{{frame: dataFrame(2, "s4", "s3")}}},
		{"too many page entries", []snapshotReply{{frame: dataFrame(2, "s3", "s4", "s5")}}},
		{"wrong cursor", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) { f.LastSkuId = "s4" })}}},
		{"data count", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) { f.TotalItems = 3 })}}},
		{"short page then more data", []snapshotReply{{frame: dataFrame(2, "s3")}, {frame: dataFrame(3, "s4")}, {frame: completeFrame(4, 4)}}},
		{"terminal with entries", []snapshotReply{{frame: mutate(completeFrame(2, 3), func(f *pb.ScanProductIndexResp) { f.List = []*pb.ProductIndexEntry{indexEntry("s3")} })}}},
		{"terminal with cursor", []snapshotReply{{frame: mutate(completeFrame(2, 2), func(f *pb.ScanProductIndexResp) { f.LastSkuId = "s2" })}}},
		{"wrong total count", []snapshotReply{{frame: completeFrame(2, 1)}}},
		{"late error after complete", []snapshotReply{{frame: completeFrame(2, 2)}, {err: errors.New("late trailer failure")}}},
		{"data after complete", []snapshotReply{{frame: completeFrame(2, 2)}, {frame: dataFrame(3, "s3")}}},
		{"duplicate complete", []snapshotReply{{frame: completeFrame(2, 2)}, {frame: completeFrame(3, 2)}}},
		{"oversized message", []snapshotReply{{frame: mutate(dataFrame(2, "s3"), func(f *pb.ScanProductIndexResp) {
			f.List[0].ProductContent = strings.Repeat("x", indexcontract.MaxPageBytes)
		})}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			replies := append([]snapshotReply{{frame: dataFrame(1, "s1", "s2")}}, tc.replies...)
			loader := NewMallProductLoader(catalogReplies(replies...), 2)
			scan, err := loader.LoadCatalog(context.Background())
			require.Error(t, err)
			require.Equal(t, CatalogScan{}, scan)
			model := &fakeVectorModel{hashes: map[string]string{"old-sku": "old-hash"}}
			idx := &fakeIndexer{}
			pipeline, err := NewPipeline(loader, nil, idx, model, "test")
			require.NoError(t, err)
			_, err = pipeline.Sync(context.Background())
			require.Error(t, err)
			require.Zero(t, idx.calls)
			require.Zero(t, model.listCalls)
			require.Zero(t, model.publishCalls)
			require.Nil(t, model.keepOnDelete)
			require.Equal(t, map[string]string{"old-sku": "old-hash"}, model.hashes)
		})
	}
}

func TestLoaderOpenFailuresNeverFallBack(t *testing.T) {
	for _, failure := range []error{status.Error(codes.Unauthenticated, "denied"), status.Error(codes.Unimplemented, "old Mall"), status.Error(codes.Unavailable, "unavailable")} {
		catalog := snapshotCatalogFunc(func(context.Context, *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
			return nil, failure
		})
		scan, err := NewMallProductLoader(catalog, 1).LoadCatalog(context.Background())
		require.ErrorIs(t, err, failure)
		require.Equal(t, CatalogScan{}, scan)
	}
	for _, catalog := range []mallCatalog{nil, snapshotCatalogFunc(func(context.Context, *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
		return nil, nil
	})} {
		_, err := NewMallProductLoader(catalog, 1).LoadCatalog(context.Background())
		require.Error(t, err)
	}
}

func TestLoaderExplicitEmptyAndPageSize(t *testing.T) {
	for _, size := range []int32{-1, 0, 1, 200, 999} {
		catalog := snapshotCatalogFunc(func(_ context.Context, in *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
			require.GreaterOrEqual(t, in.PageSize, int32(1))
			require.LessOrEqual(t, in.PageSize, int32(200))
			return &snapshotClient{replies: []snapshotReply{{frame: completeFrame(1, 0)}}}, nil
		})
		scan, err := NewMallProductLoader(catalog, size).LoadCatalog(context.Background())
		require.NoError(t, err)
		require.True(t, scan.Complete)
		require.Empty(t, scan.Documents)
	}
	// A complete empty snapshot, unlike an incomplete stream, may prune old rows.
	model := &fakeVectorModel{hashes: map[string]string{"old": "hash"}}
	p, err := NewPipeline(NewMallProductLoader(catalogReplies(snapshotReply{frame: completeFrame(1, 0)}), 1), nil, &fakeIndexer{}, model, "test")
	require.NoError(t, err)
	_, err = p.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, model.publishCalls)
	require.Empty(t, model.hashes)
}

func TestLoaderCancellationAtEveryBoundary(t *testing.T) {
	for _, when := range []string{"before open", "after open", "during receive", "after terminal"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			if when == "before open" {
				cancel()
			}
			catalog := snapshotCatalogFunc(func(ctx context.Context, _ *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
				calls++
				if when == "after open" {
					cancel()
				}
				recv := 0
				return &snapshotClient{replies: []snapshotReply{{frame: completeFrame(1, 0)}}, beforeRecv: func() {
					recv++
					if when == "during receive" || when == "after terminal" && recv == 2 {
						cancel()
					}
				}}, nil
			})
			scan, err := NewMallProductLoader(catalog, 1).LoadCatalog(ctx)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, CatalogScan{}, scan)
			if when == "before open" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestLoaderPreservesEarlierDeadlineAndCancelsStreamOnFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var streamCtx context.Context
	catalog := snapshotCatalogFunc(func(ctx context.Context, _ *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
		streamCtx = ctx
		return &snapshotClient{}, nil // premature EOF
	})
	_, err := NewMallProductLoader(catalog, 1).Load(ctx, document.Source{})
	require.Error(t, err)
	parentDeadline, _ := ctx.Deadline()
	actualDeadline, _ := streamCtx.Deadline()
	require.Equal(t, parentDeadline, actualDeadline)
	require.ErrorIs(t, streamCtx.Err(), context.Canceled)
	require.NoError(t, ctx.Err())
}

func TestLoaderRejectsAggregateLimits(t *testing.T) {
	for _, mode := range []string{"items", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			seq, count := uint32(0), 0
			stream := &generatedSnapshotClient{recv: func() (*pb.ScanProductIndexResp, error) {
				seq++
				f := dataFrame(seq)
				items := indexcontract.MaxPageSize
				if mode == "bytes" {
					items = 1
				}
				for range items {
					count++
					entry := indexEntry(fmt.Sprintf("s%08d", count))
					if mode == "bytes" {
						entry.Specs = strings.Repeat("x", 1<<20)
					}
					f.List = append(f.List, entry)
					f.LastSkuId = entry.SkuId
				}
				return f, nil
			}}
			catalog := snapshotCatalogFunc(func(context.Context, *pb.ScanProductIndexReq) (pb.ProductIndexService_ScanProductIndexClient, error) {
				return stream, nil
			})
			pageSize := int32(indexcontract.MaxPageSize)
			if mode == "bytes" {
				pageSize = 1
			}
			scan, err := NewMallProductLoader(catalog, pageSize).LoadCatalog(context.Background())
			require.Error(t, err)
			require.Equal(t, CatalogScan{}, scan)
			if mode == "items" {
				require.Equal(t, indexcontract.MaxItems+indexcontract.MaxPageSize, count)
			} else {
				require.Equal(t, 32, count)
			}
		})
	}
}

type generatedSnapshotClient struct {
	grpc.ClientStream
	recv func() (*pb.ScanProductIndexResp, error)
}

func (s *generatedSnapshotClient) Recv() (*pb.ScanProductIndexResp, error) { return s.recv() }

func TestIndexDocumentTruncatesDetailsByRune(t *testing.T) {
	require.Len(t, []rune(truncateRunes(strings.Repeat("中", detailLimit+10), detailLimit)), detailLimit)
}
