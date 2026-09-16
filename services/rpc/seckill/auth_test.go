package main

import (
	"context"
	"testing"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/interceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestSeckillRPCPermissionBoundaries(t *testing.T) {
	const secret = "seckill-permissions-test-only"
	cfg := seckillAuthConfig(secret)
	methods := map[string]bool{
		"/seckill.ActivityService/GetActivity":    false,
		"/seckill.ActivityService/ListActivities": false,
		"/seckill.SkuService/ListSkusByActivity":  false,
		"/seckill.SeckillService/AcquireToken":    false,
		"/seckill.SeckillService/SubmitOrder":     false,
		"/seckill.SeckillService/GetOrder":        false,
	}
	for method := range cfg.AdminMethods {
		methods[method] = true
	}
	if len(cfg.AdminMethods) != 10 {
		t.Fatalf("unexpected admin method count: %d", len(cfg.AdminMethods))
	}
	for _, userRole := range []int{0, 2, 100, 201} {
		ctx := context.Background()
		if userRole != 0 {
			token, err := auth.GenerateToken("user-1", secret, 3600, userRole)
			if err != nil {
				t.Fatal(err)
			}
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
		}
		for method, adminOnly := range methods {
			called := false
			_, err := interceptor.UnaryServerInterceptor(cfg)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
				called = true
				if ctx.Value(interceptor.ContextKeyUserId) != "user-1" {
					t.Fatal("missing authenticated identity")
				}
				return nil, nil
			})
			allowed := userRole == 2 || (userRole == 100 && !adminOnly)
			if called != allowed || (err == nil) != allowed {
				t.Errorf("role=%d method=%s called=%v err=%v allowed=%v", userRole, method, called, err, allowed)
			}
		}
	}
}
