package interceptor

import (
	"context"
	"errors"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/request"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestLoggingInterceptor_SuccessPassthrough(t *testing.T) {
	// 正常请求应透传到 handler 并返回结果。
	interceptor := LoggingInterceptor("")
	expectedResp := "ok"
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return expectedResp, nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	resp, err := interceptor(context.Background(), "req", info, handler)

	assert.NoError(t, err)
	assert.Equal(t, expectedResp, resp)
}

func TestLoggingInterceptor_ErrorPassthrough(t *testing.T) {
	// handler 返回的错误应被透传。
	interceptor := LoggingInterceptor("")
	testErr := errors.New("something went wrong")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, testErr
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/FailMethod"}
	resp, err := interceptor(context.Background(), "req", info, handler)

	assert.ErrorIs(t, err, testErr)
	assert.Nil(t, resp)
}

func TestLoggingInterceptor_DurationMeasured(t *testing.T) {
	// 验证耗时被正确测量。
	interceptor := LoggingInterceptor("")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return "slow", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/SlowMethod"}
	start := time.Now()
	resp, err := interceptor(context.Background(), "req", info, handler)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, "slow", resp)
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond)
}

func TestLoggingInterceptor_UserIDExtraction(t *testing.T) {
	// 有效 token 应注入 context，handler 可读取到 user_id。
	secret := "test-secret-key-for-interceptor"
	interceptor := LoggingInterceptor(secret)

	const expectedUserID = "user-123"
	token, err := auth.GenerateToken(expectedUserID, secret, 3600, 1)
	assert.NoError(t, err)

	var capturedUserID string
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedUserID = request.TryUserID(ctx)
		return "done", nil
	}

	md := metadata.Pairs("authorization", "Bearer "+token)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/AuthMethod"}
	resp, err := interceptor(ctx, "req", info, handler)

	assert.NoError(t, err)
	assert.Equal(t, "done", resp)
	assert.Equal(t, expectedUserID, capturedUserID)
}

func TestLoggingInterceptor_EmptySecretNoCrash(t *testing.T) {
	// 空 secret 时不崩溃，正常透传。
	interceptor := LoggingInterceptor("")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/NoAuth"}
	resp, err := interceptor(context.Background(), "req", info, handler)

	assert.NoError(t, err)
	assert.Equal(t, "ok", resp)
}

func TestLoggingInterceptor_ContextWithMetadata(t *testing.T) {
	// 验证 gRPC metadata 中包含 authorization 时不会崩溃。
	secret := "test-secret"
	interceptor := LoggingInterceptor(secret)

	md := metadata.Pairs("authorization", "Bearer invalid.token.here")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/WithMD"}
	resp, err := interceptor(ctx, "req", info, handler)

	assert.NoError(t, err)
	assert.Equal(t, "ok", resp)
}

func TestLoggingInterceptor_NoAuthMethodLogged(t *testing.T) {
	// 免鉴权接口（无 metadata）正常透传，不崩溃。
	interceptor := LoggingInterceptor("some-secret")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/payment.PaymentService/HandleNotify"}
	resp, err := interceptor(context.Background(), "notify", info, handler)

	assert.NoError(t, err)
	assert.Nil(t, resp)
}

func TestLoggingInterceptor_MethodInInfo(t *testing.T) {
	// 验证 FullMethod 信息被正确传递到 handler。
	interceptor := LoggingInterceptor("")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/mall.ProductService/ListProducts"}
	resp, err := interceptor(context.Background(), "req", info, handler)

	assert.NoError(t, err)
	assert.Equal(t, "ok", resp)
}
