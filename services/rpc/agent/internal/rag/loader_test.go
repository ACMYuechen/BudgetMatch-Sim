package rag

import (
	"context"
	"errors"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/cloudwego/eino/components/document"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type indexCatalogFunc func(context.Context, *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error)

func (f indexCatalogFunc) ListProductIndex(ctx context.Context, req *pb.ListProductIndexReq, _ ...grpc.CallOption) (*pb.ListProductIndexResp, error) {
	return f(ctx, req)
}

func indexEntry(id string) *pb.ProductIndexEntry {
	return &pb.ProductIndexEntry{SkuId: id, ProductId: "p1", ProductName: "键盘", SkuName: "红轴",
		Provider: "K", ProductContent: "适合办公", Specs: `{"switch":"red"}`, Price: 29900, Stock: 10, Sold: 5}
}

func TestLoaderLoadsAllPages(t *testing.T) {
	var cursors []string
	catalog := indexCatalogFunc(func(_ context.Context, in *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error) {
		cursors = append(cursors, in.Cursor)
		require.EqualValues(t, 2, in.PageSize)
		switch in.Cursor {
		case "":
			return &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s1"), indexEntry("s2")}, NextCursor: "s2"}, nil
		case "s2":
			return &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s3"), indexEntry("s4")}, Complete: true}, nil
		default:
			t.Fatalf("unexpected cursor %q", in.Cursor)
			return nil, nil
		}
	})
	docs, err := NewMallProductLoader(catalog, 2).Load(context.Background(), document.Source{URI: SourceMallProducts})
	require.NoError(t, err)
	require.Len(t, docs, 4)
	require.Equal(t, []string{"", "s2"}, cursors)
	first := docs[0]
	require.Equal(t, "s1", first.ID)
	for _, expected := range []string{"键盘 红轴", "供应商: K", "简介: 适合办公", `{"switch":"red"}`} {
		require.Contains(t, first.Content, expected)
	}
	require.NotContains(t, first.Content, "29900")
	meta, ok := CandidateFromDocument(first)
	require.True(t, ok)
	require.Equal(t, "p1", meta.ProductId)
	require.EqualValues(t, 29900, meta.PriceCents)
	require.EqualValues(t, 10, meta.Stock)
	require.Equal(t, "mall", meta.Source)
	changed := buildSkuDocument(&pb.Product{Id: "p1", Name: "键盘", Providor: "K", Content: "适合办公"},
		&pb.Sku{Id: "s1", Name: "红轴", Specs: `{"switch":"red"}`, Price: 59900, Stock: 2, Sold: 100})
	require.Equal(t, first.Content, changed.Content, "volatile metadata must not change embedding input")
}

func TestLoaderRejectsIncompletePagesAndReturnsNoPartialDocuments(t *testing.T) {
	for _, tc := range []struct {
		name string
		page *pb.ListProductIndexResp
		err  error
	}{
		{"unauthenticated", nil, status.Error(codes.Unauthenticated, "test denied")},
		{"unavailable", nil, status.Error(codes.Unavailable, "test unavailable")},
		{"nil page", nil, nil},
		{"unmarked empty", &pb.ListProductIndexResp{}, nil},
		{"nil entry", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{nil}, Complete: true}, nil},
		{"missing sku", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{{ProductId: "p1"}}, Complete: true}, nil},
		{"missing product", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{{SkuId: "s3"}}, Complete: true}, nil},
		{"duplicate from previous page", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s2")}, Complete: true}, nil},
		{"duplicate within page", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s3"), indexEntry("s3")}, Complete: true}, nil},
		{"unordered", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s4"), indexEntry("s3")}, Complete: true}, nil},
		{"oversized page", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s3"), indexEntry("s4"), indexEntry("s5")}, Complete: true}, nil},
		{"short nonterminal", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s3")}, NextCursor: "s3"}, nil},
		{"skipping cursor", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s3"), indexEntry("s4")}, NextCursor: "s5"}, nil},
		{"stalled cursor", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s3"), indexEntry("s4")}, NextCursor: "s2"}, nil},
		{"terminal with cursor", &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s3")}, NextCursor: "s3", Complete: true}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			catalog := indexCatalogFunc(func(_ context.Context, in *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error) {
				calls++
				if calls == 1 {
					return &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s1"), indexEntry("s2")}, NextCursor: "s2"}, nil
				}
				return tc.page, tc.err
			})
			docs, err := NewMallProductLoader(catalog, 2).Load(context.Background(), document.Source{})
			require.Error(t, err)
			require.Nil(t, docs)
			require.Equal(t, 2, calls)
		})
	}
}

func TestLoaderExplicitEmptyAndPageSize(t *testing.T) {
	for _, size := range []int32{-1, 0, 1, 200, 999} {
		catalog := indexCatalogFunc(func(_ context.Context, in *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error) {
			require.Greater(t, in.PageSize, int32(0))
			require.LessOrEqual(t, in.PageSize, int32(200))
			return &pb.ListProductIndexResp{Complete: true}, nil
		})
		docs, err := NewMallProductLoader(catalog, size).Load(context.Background(), document.Source{})
		require.NoError(t, err)
		require.Empty(t, docs)
	}
	_, err := NewMallProductLoader(nil, 1).Load(context.Background(), document.Source{})
	require.Error(t, err)
}

func TestLoaderHonorsCancellation(t *testing.T) {
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		if before {
			cancel()
		}
		catalog := indexCatalogFunc(func(_ context.Context, _ *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error) {
			calls++
			cancel()
			return &pb.ListProductIndexResp{Complete: true}, nil
		})
		docs, err := NewMallProductLoader(catalog, 1).Load(ctx, document.Source{})
		cancel()
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, docs)
		if before {
			require.Zero(t, calls)
		} else {
			require.Equal(t, 1, calls)
		}
	}
}

func TestIndexReadFailurePreservesExistingVectors(t *testing.T) {
	for _, failure := range []error{status.Error(codes.Unauthenticated, "denied"), errors.New("unavailable")} {
		catalog := indexCatalogFunc(func(_ context.Context, in *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error) {
			if in.Cursor == "" {
				return &pb.ListProductIndexResp{List: []*pb.ProductIndexEntry{indexEntry("s1")}, NextCursor: "s1"}, nil
			}
			return nil, failure
		})
		model := &fakeVectorModel{hashes: map[string]string{"old-sku": "old-hash"}}
		idx := &fakeIndexer{}
		pipeline, err := NewPipeline(NewMallProductLoader(catalog, 1), nil, idx, model, "test")
		require.NoError(t, err)
		_, err = pipeline.Sync(context.Background())
		require.ErrorIs(t, err, failure)
		require.Empty(t, idx.stored)
		require.Empty(t, model.metadataUpdates)
		require.Nil(t, model.keepOnDelete, "failed scan must not prune, including DeleteNotIn(empty)")
		require.Equal(t, map[string]string{"old-sku": "old-hash"}, model.hashes)
	}
}

func TestIndexDocumentTruncatesDetailsByRune(t *testing.T) {
	text := strings.Repeat("中", detailLimit+10)
	require.Len(t, []rune(truncateRunes(text, detailLimit)), detailLimit)
}
