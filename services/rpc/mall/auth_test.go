package main

import (
	"context"
	"net"
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
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	indexMethod   = "/mall.ProductIndexService/ListProductIndex"
	paymentMethod = "/mall.OrderService/ConfirmPayment"
)

type indexReader struct{ calls atomic.Int32 }

func (r *indexReader) ListPage(context.Context, string, int) ([]product_index.Entry, error) {
	r.calls.Add(1)
	return []product_index.Entry{{SkuId: "s1", ProductId: "p1", ProductName: "test", Price: 100, Stock: 1}}, nil
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
		}))
	svcCtx := &svc.ServiceContext{ProductIndexStore: reader}
	pb.RegisterProductIndexServiceServer(server, productindexservice.NewProductIndexServiceServer(svcCtx))
	pb.RegisterProductServiceServer(server, productservice.NewProductServiceServer(svcCtx))
	pb.RegisterOrderServiceServer(server, orderservice.NewOrderServiceServer(svcCtx))
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
		"CreateProduct": true, "UpdateProduct": true, "DeleteProduct": true,
		"CreateSku": true, "UpdateSku": true, "DeleteSku": true,
		"GetOrderOutboxStats": true, "ListOrderOutbox": true, "GetOrderOutbox": true, "ReplayOrderOutbox": true,
	}
	services := pb.File_services_rpc_mall_proto_mall_proto.Services()
	for i := 0; i < services.Len(); i++ {
		desc := services.Get(i)
		for j := 0; j < desc.Methods().Len(); j++ {
			method := desc.Methods().Get(j)
			full := "/" + string(desc.FullName()) + "/" + string(method.Name())
			ordinary := full != indexMethod && full != paymentMethod
			for _, tc := range []struct {
				name, token string
				allow       bool
			}{
				{"missing", "", false}, {"user", user, ordinary && !adminOnly[string(method.Name())]},
				{"admin", admin, ordinary}, {"payment", payment, full == paymentMethod},
				{"index", index, full == indexMethod}, {"index signed by payment key", wrongKey, false},
			} {
				t.Run(full+"/"+tc.name, func(t *testing.T) {
					before := allowed.Load()
					err := conn.Invoke(bearerCtx(t, tc.token), full, &emptypb.Empty{}, &emptypb.Empty{})
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
	require.Zero(t, reader.calls.Load())
	payment, err := serviceauth.GenerateToken(serviceauth.ServicePayment, serviceauth.ServiceMall, c.ServiceAuth.Secret, time.Minute)
	require.NoError(t, err)
	require.NoError(t, conn.Invoke(bearerCtx(t, payment), paymentMethod, &emptypb.Empty{}, &emptypb.Empty{}))
}
