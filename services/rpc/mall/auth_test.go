package main

import (
	"context"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/internal/config"
	orderservice "budgetmatch-sim/services/rpc/mall/internal/server/orderservice"
	productindexservice "budgetmatch-sim/services/rpc/mall/internal/server/productindexservice"
	productservice "budgetmatch-sim/services/rpc/mall/internal/server/productservice"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	indexMethod     = "/mall.ProductIndexService/ListProductIndex"
	indexScanMethod = "/mall.ProductIndexService/ScanProductIndex"
	paymentMethod   = "/mall.OrderService/ConfirmPayment"
)

type indexReader struct {
	calls atomic.Int32
	scan  func(context.Context, int, func([]product_index.Entry) error) error
}

func (r *indexReader) ListPage(context.Context, string, int) ([]product_index.Entry, error) {
	r.calls.Add(1)
	return []product_index.Entry{{SkuId: "s1", ProductId: "p1", ProductName: "test", Price: 100, Stock: 1}}, nil
}

func (r *indexReader) ScanSnapshot(ctx context.Context, size int, visit func([]product_index.Entry) error) error {
	if r.scan != nil {
		return r.scan(ctx, size, visit)
	}
	rows, err := r.ListPage(ctx, "", 1)
	if err != nil {
		return err
	}
	return visit(rows)
}

func testMallConfig() config.Config {
	var c config.Config
	c.JwtAuth.Secret = "user-jwt-unit-test-independent-secret"
	c.ServiceAuth.Secret = "payment-unit-test-independent-secret"
	c.IndexAuth.Secret = "agent-index-unit-test-independent-secret"
	return c
}

// Real protobuf registration + production auth policy + real index logic over an
// in-memory Reader. Other business handlers stop at an auth probe: no DB or MQ.
func authTestConn(t *testing.T, c config.Config) (*grpc.ClientConn, *indexReader, *atomic.Int32) {
	t.Helper()
	reader := &indexReader{}
	allowed := &atomic.Int32{}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(interceptor.UnaryServerInterceptor(c.RPCAuthConfig()),
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			allowed.Add(1)
			if info.FullMethod == indexMethod {
				return handler(ctx, req)
			}
			return &emptypb.Empty{}, nil
		}), grpc.ChainStreamInterceptor(interceptor.StreamServerInterceptor(c.RPCAuthConfig()),
		func(srv interface{}, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			allowed.Add(1)
			return handler(srv, stream)
		}))
	svcCtx := &svc.ServiceContext{ProductIndexStore: reader, ProductIndexSnapshots: reader}
	pb.RegisterProductIndexServiceServer(server, productindexservice.NewProductIndexServiceServer(svcCtx))
	pb.RegisterProductServiceServer(server, productservice.NewProductServiceServer(svcCtx))
	pb.RegisterOrderServiceServer(server, orderservice.NewOrderServiceServer(svcCtx))
	// Match main.go's development/test registration; reflection is also a stream.
	reflection.Register(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///mall-auth-test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn, reader, allowed
}

func mintIndexToken(t *testing.T, secret, caller, audience, purpose string) string {
	t.Helper()
	token, err := serviceauth.GenerateScopedToken(caller, audience, purpose, secret, time.Minute)
	require.NoError(t, err)
	return token
}

func bearerCtx(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if token != "" {
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
	}
	return ctx
}

func TestMallRPCMethodAuthMatrix(t *testing.T) {
	c := testMallConfig()
	conn, _, allowed := authTestConn(t, c)
	user, err := auth.GenerateToken("user-1", c.JwtAuth.Secret, 3600, role.RoleUser)
	require.NoError(t, err)
	admin, err := auth.GenerateToken("admin-1", c.JwtAuth.Secret, 3600, role.RoleAdmin)
	require.NoError(t, err)
	payment, err := serviceauth.GenerateToken(serviceauth.ServicePayment, serviceauth.ServiceMall, c.ServiceAuth.Secret, time.Minute)
	require.NoError(t, err)
	index := mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	wrongKey := mintIndexToken(t, c.ServiceAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	// Explicit expected set, independent of production configuration.
	adminOnly := map[string]bool{
		"UpdateOrderStatus": true,
		"CreateProduct":     true, "UpdateProduct": true, "DeleteProduct": true,
		"CreateSku": true, "UpdateSku": true, "DeleteSku": true,
		"GetOrderOutboxStats": true, "ListOrderOutbox": true, "GetOrderOutbox": true, "ReplayOrderOutbox": true,
	}
	services := pb.File_services_rpc_mall_proto_mall_proto.Services()
	for i := 0; i < services.Len(); i++ {
		desc := services.Get(i)
		for j := 0; j < desc.Methods().Len(); j++ {
			method := desc.Methods().Get(j)
			full := "/" + string(desc.FullName()) + "/" + string(method.Name())
			isIndex := full == indexMethod || full == indexScanMethod
			ordinary := !isIndex && full != paymentMethod
			for _, tc := range []struct {
				name, token string
				allow       bool
			}{
				{"missing", "", false}, {"user", user, ordinary && !adminOnly[string(method.Name())]},
				{"admin", admin, ordinary}, {"payment", payment, full == paymentMethod},
				{"index", index, isIndex}, {"index signed by payment key", wrongKey, false},
			} {
				t.Run(full+"/"+tc.name, func(t *testing.T) {
					before := allowed.Load()
					var err error
					if method.IsStreamingServer() {
						require.Equal(t, indexScanMethod, full, "add an explicit invocation for any new streaming method")
						err = drainIndexSnapshot(bearerCtx(t, tc.token), conn)
					} else {
						err = conn.Invoke(bearerCtx(t, tc.token), full, &emptypb.Empty{}, &emptypb.Empty{})
					}
					if tc.allow {
						require.NoError(t, err)
						require.Equal(t, before+1, allowed.Load())
					} else {
						require.Equal(t, codes.Unauthenticated, status.Code(err))
						require.Equal(t, before, allowed.Load(), "rejected request must not reach business logic")
					}
				})
			}
		}
	}
}

func TestIndexRPCRejectsInvalidCredentials(t *testing.T) {
	c := testMallConfig()
	conn, reader, _ := authTestConn(t, c)
	valid := mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	claims, err := serviceauth.ValidateScopedToken(valid, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	require.NoError(t, err)
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(c.IndexAuth.Secret))
	require.NoError(t, err)
	unscoped, err := serviceauth.GenerateToken(serviceauth.ServiceAgent, serviceauth.ServiceMall, c.IndexAuth.Secret, time.Minute)
	require.NoError(t, err)
	userWithIndexKey, err := auth.GenerateToken("user-1", c.IndexAuth.Secret, 3600, role.RoleAdmin)
	require.NoError(t, err)
	for _, tc := range []struct{ name, token string }{
		{"expired", expired}, {"unscoped", unscoped}, {"user claims signed by index key", userWithIndexKey},
		{"wrong caller", mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServicePayment, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)},
		{"wrong audience", mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServicePayment, serviceauth.PurposeProductIndexRead)},
		{"wrong purpose", mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, "order:write")},
		{"wrong secret", mintIndexToken(t, "different-key", serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := pb.NewProductIndexServiceClient(conn).ListProductIndex(bearerCtx(t, tc.token), &pb.ListProductIndexReq{})
			require.Equal(t, codes.Unauthenticated, status.Code(err))
			require.Nil(t, resp)
			require.Equal(t, codes.Unauthenticated, status.Code(drainIndexSnapshot(bearerCtx(t, tc.token), conn)))
			require.Zero(t, reader.calls.Load())
		})
	}
	resp, err := pb.NewProductIndexServiceClient(conn).ListProductIndex(bearerCtx(t, valid), &pb.ListProductIndexReq{PageSize: 2})
	require.NoError(t, err)
	require.True(t, resp.Complete)
	require.Len(t, resp.List, 1)
	require.Equal(t, "s1", resp.List[0].SkuId)
	require.EqualValues(t, 1, reader.calls.Load())
}

func TestUnconfiguredIndexKeyDoesNotFallBackOrBreakPayment(t *testing.T) {
	c := testMallConfig()
	c.IndexAuth.Secret = ""
	conn, reader, _ := authTestConn(t, c)
	index := mintIndexToken(t, c.ServiceAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	_, err := pb.NewProductIndexServiceClient(conn).ListProductIndex(bearerCtx(t, index), &pb.ListProductIndexReq{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Equal(t, codes.Unauthenticated, status.Code(drainIndexSnapshot(bearerCtx(t, index), conn)))
	require.Zero(t, reader.calls.Load())
	payment, err := serviceauth.GenerateToken(serviceauth.ServicePayment, serviceauth.ServiceMall, c.ServiceAuth.Secret, time.Minute)
	require.NoError(t, err)
	require.NoError(t, conn.Invoke(bearerCtx(t, payment), paymentMethod, &emptypb.Empty{}, &emptypb.Empty{}))
}

func drainIndexSnapshot(ctx context.Context, conn *grpc.ClientConn) error {
	stream, err := pb.NewProductIndexServiceClient(conn).ScanProductIndex(ctx, &pb.ScanProductIndexReq{})
	if err != nil {
		return err
	}
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func TestSnapshotRPCRequiresBoundedTransportDeadline(t *testing.T) {
	c := testMallConfig()
	conn, reader, _ := authTestConn(t, c)
	token := mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	require.Equal(t, codes.InvalidArgument, status.Code(drainIndexSnapshot(ctx, conn)))
	longCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	require.Equal(t, codes.InvalidArgument, status.Code(drainIndexSnapshot(longCtx, conn)))
	// No initial request at all: admission must reject before the generated
	// streaming handler can block in RecvMsg. A logic-only check is too late.
	for _, base := range []context.Context{ctx, longCtx} {
		streamCtx, cancelStream := context.WithCancel(base)
		defer cancelStream()
		stream, err := conn.NewStream(streamCtx, &grpc.StreamDesc{ServerStreams: true}, indexScanMethod)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { done <- stream.RecvMsg(&pb.ScanProductIndexResp{}) }()
		select {
		case err := <-done:
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		case <-time.After(2 * time.Second):
			t.Fatal("unbounded stream blocked before its first request")
		}
		cancelStream()
	}
	require.Zero(t, reader.calls.Load())
}

func TestSnapshotRPCSlowConsumerDoesNotKeepSnapshotAfterDeadline(t *testing.T) {
	c := testMallConfig()
	conn, reader, _ := authTestConn(t, c)
	started, released := make(chan struct{}), make(chan struct{})
	reader.scan = func(ctx context.Context, _ int, visit func([]product_index.Entry) error) error {
		close(started)
		defer close(released)
		// Enough to exceed HTTP/2 flow-control buffering if the client never Recvs.
		payload := strings.Repeat("x", 1<<20)
		for i := range 20 {
			if err := visit([]product_index.Entry{{SkuId: string(rune('a' + i)), ProductId: "p1", Specs: payload}}); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
	token := mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token)), 500*time.Millisecond)
	defer cancel()
	_, err := pb.NewProductIndexServiceClient(conn).ScanProductIndex(ctx, &pb.ScanProductIndexReq{PageSize: 1})
	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot never started")
	}
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("slow consumer retained snapshot after deadline")
	}
}

func TestMallReflectionStreamUsesDefaultUserPolicy(t *testing.T) {
	c := testMallConfig()
	conn, reader, _ := authTestConn(t, c)
	user, err := auth.GenerateToken("user", c.JwtAuth.Secret, 3600, role.RoleUser)
	require.NoError(t, err)
	index := mintIndexToken(t, c.IndexAuth.Secret, serviceauth.ServiceAgent, serviceauth.ServiceMall, serviceauth.PurposeProductIndexRead)
	for _, tc := range []struct {
		name, token string
		allow       bool
	}{{"missing", "", false}, {"user", user, true}, {"index service", index, false}} {
		t.Run(tc.name, func(t *testing.T) {
			stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(bearerCtx(t, tc.token))
			require.NoError(t, err)
			defer stream.CloseSend()
			_ = stream.Send(&reflectionpb.ServerReflectionRequest{MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{ListServices: ""}})
			resp, err := stream.Recv()
			if tc.allow {
				require.NoError(t, err)
				require.NotNil(t, resp.GetListServicesResponse())
			} else {
				require.Equal(t, codes.Unauthenticated, status.Code(err))
			}
		})
	}
	require.Zero(t, reader.calls.Load())
}
