package interceptor

import (
	"context"
	"strings"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// contextKey 用于在 context 中存储认证信息
type contextKey string

const ContextKeyUser contextKey = "user"

var noAuthMethods = map[string]struct{}{
	"/auth.AuthService/UsernameLogin": {},
	"/auth.AuthService/EmailLogin":    {},
	"/auth.AuthService/EmailRegister": {},
	"/auth.AuthService/ValidateToken": {},
	"/auth.AuthService/SendCode":      {},
	"/auth.AuthService/LoginByCode":   {},
}

// AuthInterceptor 从 gRPC metadata 中提取 Authorization token，验证并注入完整用户信息
func AuthInterceptor(svcCtx *svc.ServiceContext) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 不需要认证的接口白名单
		if _, ok := noAuthMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		// 从 metadata 取 Authorization
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, errors.ErrInvalidToken
		}

		var tokenString string
		if vals := md.Get("authorization"); len(vals) > 0 {
			tokenString = trimBearerToken(vals[0])
		}

		if tokenString == "" {
			return nil, errors.ErrInvalidToken
		}

		// 验证 token
		_, err := auth.ValidateToken(tokenString, svcCtx.Config.Auth.Secret)
		if err != nil {
			return nil, errors.ErrInvalidToken
		}

		// 获取用户 ID
		userId, err := auth.GetUserIdFromToken(tokenString, svcCtx.Config.Auth.Secret)
		if err != nil {
			return nil, errors.ErrInvalidToken
		}

		// 查库获取完整用户信息
		u, err := svcCtx.UserStore.FindOne(ctx, userId)
		if err != nil {
			return nil, errors.ErrDatabase
		}
		if u == nil {
			return nil, errors.ErrUserNotFound
		}

		// 注入完整用户到 context
		ctx = context.WithValue(ctx, ContextKeyUser, u)

		return handler(ctx, req)
	}
}

func trimBearerToken(authorization string) string {
	authorization = strings.TrimSpace(authorization)
	if authorization == "" {
		return ""
	}
	if len(authorization) >= 7 && strings.EqualFold(authorization[:7], "Bearer ") {
		return strings.TrimSpace(authorization[7:])
	}
	return authorization
}
