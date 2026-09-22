package interceptor

import (
	"context"
	"strings"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// contextKey 用于在 context 中存储认证信息
type contextKey string

const ContextKeyUser contextKey = "user"

var adminMethods = map[string]struct{}{
	"/auth.UserService/GetUserById":    {},
	"/auth.UserService/ListUsers":      {},
	"/auth.UserService/UpdateUserInfo": {},
	"/auth.UserService/DeleteUser":     {},
}

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
			return nil, errors.InvalidToken
		}

		var tokenString string
		if vals := md.Get("authorization"); len(vals) > 0 {
			tokenString = trimBearerToken(vals[0])
		}

		if tokenString == "" {
			return nil, errors.InvalidToken
		}

		// 验证 token
		_, err := auth.ValidateToken(tokenString, svcCtx.Config.JwtAuth.Secret)
		if err != nil {
			return nil, errors.InvalidToken
		}

		// 获取用户 ID
		userId, err := auth.GetUserIdFromToken(tokenString, svcCtx.Config.JwtAuth.Secret)
		if err != nil {
			return nil, errors.InvalidToken
		}

		// 查库获取完整用户信息
		u, err := svcCtx.UserStore.FindOne(ctx, userId)
		if err != nil {
			return nil, errors.Database
		}
		if u == nil {
			return nil, errors.UserNotFound
		}

		// 角色级鉴权：拒绝非全局用户身份（如已注销/封禁等异常角色）
		if u.Status != user.StatusNormal || !role.IsGlobalUserRole(int64(u.Role)) {
			return nil, errors.Unauthorized
		}

		tokenRole, err := auth.GetUserRoleFromToken(tokenString, svcCtx.Config.JwtAuth.Secret)
		if err != nil || tokenRole != u.Role {
			return nil, errors.InvalidToken
		}
		if _, adminOnly := adminMethods[info.FullMethod]; adminOnly && !role.IsGlobalAdminRole(int64(u.Role)) {
			return nil, errors.Unauthorized
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
