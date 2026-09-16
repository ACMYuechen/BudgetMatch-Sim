package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"budgetmatch-sim/cmd/app/internal/middleware"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	authpb "budgetmatch-sim/services/rpc/auth/pb"
	"budgetmatch-sim/services/rpc/mall/client/productservice"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/go-playground/validator/v10"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"google.golang.org/grpc"
)

type routeAuthClient struct{ authservice.AuthService }

func (*routeAuthClient) ValidateToken(_ context.Context, req *authpb.ValidateTokenReq, _ ...grpc.CallOption) (*authpb.ValidateTokenResp, error) {
	if req.Token != "valid-token" {
		return nil, fmt.Errorf("invalid test token")
	}
	return &authpb.ValidateTokenResp{User: &authpb.UserInfo{Id: "user-1", Role: 100}}, nil
}

type routeProductClient struct {
	productservice.ProductService
	calls int
}

func (c *routeProductClient) GetSku(ctx context.Context, req *pb.GetSkuReq, _ ...grpc.CallOption) (*pb.GetSkuResp, error) {
	c.calls++
	if ctx.Value("user_id") != "user-1" || ctx.Value("token") != "valid-token" {
		return nil, fmt.Errorf("missing authenticated context")
	}
	return &pb.GetSkuResp{Sku: &pb.Sku{Id: req.Id, ProductId: "product-1", Price: 19900, Status: 1}}, nil
}

func TestSkuDetailRouteRequiresAuthentication(t *testing.T) {
	var config rest.RestConf
	if err := conf.FillDefault(&config); err != nil {
		t.Fatal(err)
	}
	config.Name = "sku-route-test"
	server, err := rest.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	client := &routeProductClient{}
	RegisterHandlers(server, &svc.ServiceContext{
		Validator: validator.New(), MallProductClient: client,
		AuthMiddleware:             middleware.NewAuthMiddleware(&routeAuthClient{}).Handle,
		SeckillRateLimitMiddleware: func(next http.HandlerFunc) http.HandlerFunc { return next },
	})
	var handler http.HandlerFunc
	for _, route := range server.Routes() {
		if route.Path == "/api/mall/skus/:id" && route.Method == http.MethodGet {
			handler = route.Handler
		}
	}
	if handler == nil {
		t.Fatal("SKU detail GET route not registered")
	}
	for _, tc := range []struct {
		token  string
		status int
		calls  int
	}{
		{"", http.StatusUnauthorized, 0},
		{"Bearer invalid-token", http.StatusUnauthorized, 0},
		{"Bearer valid-token", http.StatusOK, 1},
	} {
		client.calls = 0
		request := httptest.NewRequest(http.MethodGet, "/api/mall/skus/sku-1", nil)
		request.Header.Set("Authorization", tc.token)
		request = pathvar.WithVars(request, map[string]string{"id": "sku-1"})
		recorder := httptest.NewRecorder()
		handler(recorder, request)
		if recorder.Code != tc.status || client.calls != tc.calls {
			t.Fatalf("status=%d calls=%d, want status=%d calls=%d: %s", recorder.Code, client.calls, tc.status, tc.calls, recorder.Body.String())
		}
		if tc.status == http.StatusOK {
			var response types.MallSkuDetailResp
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Sku.Id != "sku-1" || response.Sku.ProductId != "product-1" {
				t.Fatalf("incorrect SKU mapping: %+v", response)
			}
		}
	}
}
