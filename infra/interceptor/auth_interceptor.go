// Package interceptor 提供通用的 gRPC 认证拦截器，供各 RPC 服务复用。
package interceptor

import (
	"context"
	"strings"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// contextKey 用于在 context 中注入认证信息。
type contextKey string

const (
	ContextKeyUserID contextKey = "user_id"
	ContextKeyRole   contextKey = "role"
	ContextKeyToken  contextKey = "token"
)

// AuthConfig 配置认证拦截器的行为。
type AuthConfig struct {
	// Secret 是 JWT 签名密钥。
	Secret string
	// NoAuthMethods 是跳过认证的方法白名单（full gRPC method name）。
	NoAuthMethods map[string]struct{}
	// AdminMethods 是需要全局管理员角色（role 1-99）的方法集合。
	// 未在此集合且不在 NoAuthMethods 中的方法默认要求全局用户角色（role 1-199）。
	AdminMethods map[string]struct{}
}

// UnaryServerInterceptor 返回一个 gRPC 一元拦截器，完成 JWT 校验与角色鉴权：
//
//  1. 白名单方法直接放行
//  2. 从 gRPC metadata 提取 Authorization: Bearer <token>
//  3. 验证 JWT 签名并从 claims 提取 user_id、role
//  4. admin 方法要求 role.IsGlobalAdminRole，其余要求 role.IsGlobalUserRole
//  5. 将 user_id、role 与原始 token 注入 context（token 供下游 RPC 传播）
func UnaryServerInterceptor(cfg AuthConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// 1. 白名单放行
		if _, ok := cfg.NoAuthMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		// 2. 提取 token
		tokenString, err := extractToken(ctx)
		if err != nil {
			return nil, err
		}

		// 3. 验证 JWT 签名
		if _, err := auth.ValidateToken(tokenString, cfg.Secret); err != nil {
			return nil, errors.InvalidToken
		}

		// 4. 提取 user_id
		userID, err := auth.GetUserIdFromToken(tokenString, cfg.Secret)
		if err != nil {
			return nil, errors.InvalidToken
		}

		// 5. 提取 role
		userRole, err := auth.GetUserRoleFromToken(tokenString, cfg.Secret)
		if err != nil {
			return nil, errors.InvalidToken
		}

		// 6. 角色鉴权
		if _, isAdmin := cfg.AdminMethods[info.FullMethod]; isAdmin {
			if !role.IsGlobalAdminRole(int64(userRole)) {
				return nil, errors.Unauthorized
			}
		} else {
			if !role.IsGlobalUserRole(int64(userRole)) {
				return nil, errors.Unauthorized
			}
		}

		// 7. 注入 context（user_id、role、原始 token，供下游 RPC 调用传播）
		ctx = context.WithValue(ctx, ContextKeyUserID, userID)
		ctx = context.WithValue(ctx, ContextKeyRole, int64(userRole))
		ctx = context.WithValue(ctx, ContextKeyToken, tokenString)

		return handler(ctx, req)
	}
}

// extractToken 从 gRPC metadata 中提取并清理 Bearer token。
func extractToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.InvalidToken
	}

	var raw string
	if vals := md.Get("authorization"); len(vals) > 0 {
		raw = vals[0]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.InvalidToken
	}
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "Bearer ") {
		return strings.TrimSpace(raw[7:]), nil
	}
	return raw, nil
}
