// Package interceptor 提供通用的 gRPC 拦截器，供各 RPC 服务复用。
package interceptor

import (
	"context"
	"strings"
	"time"

	"budgetmatch-sim/infra/auth"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// LoggingInterceptor 返回一个 gRPC 一元拦截器，记录每次 RPC 调用的入口与出口信息，
// 包括方法名、耗时、错误，与 logic 层细粒度业务错误日志形成互补。
// secret 为 JWT 签名密钥，非空时从 gRPC metadata 提取 user_id 写入日志。
func LoggingInterceptor(secret string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		userID := extractUserIDFromMD(ctx, secret)

		logFields := []logx.LogField{
			logx.Field("method", info.FullMethod),
		}
		if userID != "" {
			logFields = append(logFields, logx.Field("user_id", userID))
		}

		logx.WithContext(ctx).Infow("request start", logFields...)

		resp, err := handler(ctx, req)

		duration := time.Since(start).Milliseconds()
		endFields := []logx.LogField{
			logx.Field("method", info.FullMethod),
			logx.Field("duration_ms", duration),
		}
		if userID != "" {
			endFields = append(endFields, logx.Field("user_id", userID))
		}

		if err != nil {
			endFields = append(endFields, logx.Field("error", err.Error()))
			logx.WithContext(ctx).Errorw("request end", endFields...)
		} else {
			logx.WithContext(ctx).Infow("request end", endFields...)
		}

		return resp, err
	}
}

// extractUserIDFromMD 从 gRPC metadata 的 Authorization 字段提取 user_id。
// 仅在 secret 非空时尝试，解析失败静默返回空字符串。
func extractUserIDFromMD(ctx context.Context, secret string) string {
	if secret == "" {
		return ""
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	var raw string
	if vals := md.Get("authorization"); len(vals) > 0 {
		raw = vals[0]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "Bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	if raw == "" {
		return ""
	}

	userID, err := auth.GetUserIdFromToken(raw, secret)
	if err != nil {
		return ""
	}
	return userID
}
