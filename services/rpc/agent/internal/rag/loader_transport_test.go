package rag

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/indexcontract"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type snapshotRPCServer struct {
	pb.UnimplementedProductIndexServiceServer
	scan        func(*pb.ScanProductIndexReq, pb.ProductIndexService_ScanProductIndexServer) error
	legacyCalls atomic.Int32
}

func (s *snapshotRPCServer) ScanProductIndex(req *pb.ScanProductIndexReq, stream pb.ProductIndexService_ScanProductIndexServer) error {
	if s.scan == nil {
		return status.Error(codes.Unimplemented, "legacy Mall")
	}
	return s.scan(req, stream)
}
func (s *snapshotRPCServer) ListProductIndex(context.Context, *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error) {
	s.legacyCalls.Add(1)
	return &pb.ListProductIndexResp{Complete: true}, nil
}
func snapshotRPCClient(t *testing.T, s *snapshotRPCServer) pb.ProductIndexServiceClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	cfg := interceptor.AuthConfig{ServiceSecrets: map[string]string{"index": indexTestSecret}, ServiceMethods: map[string]interceptor.ServiceMethodPolicy{
		indexScanMethod: {Caller: serviceauth.ServiceAgent, Audience: serviceauth.ServiceMall, SecretName: "index", Purpose: serviceauth.PurposeProductIndexRead},
	}}
	server := grpc.NewServer(grpc.StreamInterceptor(interceptor.StreamServerInterceptor(cfg)))
	pb.RegisterProductIndexServiceServer(server, s)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///snapshot-test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithStreamInterceptor(IndexStreamAuthInterceptor(indexTestSecret)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewProductIndexServiceClient(conn)
}

func TestLoaderRealGRPCCompletionAndLateStatus(t *testing.T) {
	for _, mode := range []string{"complete", "empty", "missing terminal", "late failure", "unimplemented", "extra frame"} {
		t.Run(mode, func(t *testing.T) {
			s := &snapshotRPCServer{}
			if mode != "unimplemented" {
				s.scan = func(req *pb.ScanProductIndexReq, stream pb.ProductIndexService_ScanProductIndexServer) error {
					if stream.Context().Value(interceptor.ContextKeyServiceName) != serviceauth.ServiceAgent {
						return status.Error(codes.Unauthenticated, "no service identity")
					}
					deadline, ok := stream.Context().Deadline()
					if !ok || time.Until(deadline) > indexcontract.MaxDuration {
						return status.Error(codes.InvalidArgument, "unbounded stream")
					}
					if req.PageSize != 1 {
						return status.Error(codes.InvalidArgument, "wrong page size")
					}
					if mode == "empty" {
						return stream.Send(completeFrame(1, 0))
					}
					if err := stream.Send(dataFrame(1, "s1")); err != nil {
						return err
					}
					if mode == "missing terminal" {
						return nil
					}
					if err := stream.Send(completeFrame(2, 1)); err != nil {
						return err
					}
					if mode == "late failure" {
						return status.Error(codes.Internal, "late failure")
					}
					if mode == "extra frame" {
						return stream.Send(dataFrame(3, "s2"))
					}
					return nil
				}
			}
			client := snapshotRPCClient(t, s)
			model := &fakeVectorModel{hashes: map[string]string{"old": "hash"}}
			idx := &fakeIndexer{}
			p, err := NewPipeline(NewMallProductLoader(client, 1), nil, idx, model, "test")
			require.NoError(t, err)
			_, err = p.Sync(context.Background())
			if mode == "complete" || mode == "empty" {
				require.NoError(t, err)
				require.Equal(t, 1, model.publishCalls)
				require.NotContains(t, model.hashes, "old")
				if mode == "complete" {
					require.Contains(t, model.hashes, "s1")
				} else {
					require.Empty(t, model.hashes)
				}
			} else {
				require.Error(t, err)
				require.Zero(t, model.publishCalls)
				require.Zero(t, idx.calls)
				require.Equal(t, map[string]string{"old": "hash"}, model.hashes)
			}
			require.Zero(t, s.legacyCalls.Load(), "old Mall must never trigger a live-pagination fallback")
		})
	}
}

func TestLoaderCancelsRealStreamAfterProtocolFailure(t *testing.T) {
	released := make(chan struct{})
	s := &snapshotRPCServer{scan: func(_ *pb.ScanProductIndexReq, stream pb.ProductIndexService_ScanProductIndexServer) error {
		defer close(released)
		if err := stream.Send(dataFrame(99, "s1")); err != nil {
			return err
		}
		<-stream.Context().Done()
		return stream.Context().Err()
	}}
	_, err := NewMallProductLoader(snapshotRPCClient(t, s), 1).LoadCatalog(context.Background())
	require.Error(t, err)
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("client failure leaked server stream")
	}
}
