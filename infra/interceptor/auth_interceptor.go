// Package interceptor 提供通用的 gRPC 认证拦截器，供各 RPC 服务复用。
package interceptor

import (
	"budgetmatch-sim/infra/serviceauth"
	"context"
	"strings"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// contextKey 用于在 context 中注入认证信息。
type contextKey string

const (
	ContextKeyUserId         contextKey = "user_id"
	ContextKeyRole           contextKey = "role"
	ContextKeyToken          contextKey = "token"
	ContextKeyServiceName    contextKey = "service_name"
	ContextKeyServicePurpose contextKey = "service_purpose"
)

// ServiceMethodPolicy 描述某个 RPC 方法允许的服务调用方
type ServiceMethodPolicy struct {
	Caller   string
	Audience string
	// SecretName 非空时只取 ServiceSecrets 中的指定密钥；缺失时拒绝，绝不回退。
	SecretName string
	Purpose    string
	// MaxStreamDuration > 0 requires a caller-supplied transport deadline before
	// the streaming handler can receive any request. Unary policies ignore it.
	MaxStreamDuration time.Duration
}

// AuthConfig 配置认证拦截器的行为。
type AuthConfig struct {
	// Secret 是 JWT 签名密钥。
	Secret string
	// NoAuthMethods 是跳过认证的方法白名单（full gRPC method name）。
	NoAuthMethods map[string]struct{}
	// AdminMethods 是需要全局管理员角色（role 1-99）的方法集合。
	// 未在此集合且不在 NoAuthMethods 中的方法默认要求全局用户角色（role 1-199）。
	AdminMethods map[string]struct{}
	// ServiceSecret 使用独立于用户 JWT 的服务认证密钥
	ServiceSecret string
	// ServiceSecrets 供新增服务策略选择独立密钥，ServiceSecret 保留兼容旧策略。
	ServiceSecrets map[string]string
	// ServiceMethods 中的方法只能使用服务 JWT 调用
	ServiceMethods map[string]ServiceMethodPolicy
	// StreamMaxDurations requires a caller-supplied transport deadline for each
	// listed stream (including user methods). If a service policy also sets a
	// limit, the stricter positive limit wins. Unary calls ignore this map.
	StreamMaxDurations map[string]time.Duration
}

// UnaryServerInterceptor 返回一个 gRPC 一元拦截器，完成 JWT 校验与角色鉴权：
//
// 服务专用方法优先匹配独立策略，只接受服务 JWT，不进入下面的用户/白名单流程。
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
		// 服务专用方法只接受服务 JWT ，不进入普通用户 JWT 验证流程
		if policy, ok := cfg.ServiceMethods[info.FullMethod]; ok {
			tokenString, err := extractToken(ctx)
			if err != nil {
				return nil, err
			}
			secret := cfg.ServiceSecret
			if policy.SecretName != "" {
				secret = cfg.ServiceSecrets[policy.SecretName]
			}
			var claims *serviceauth.Claims
			if policy.Purpose != "" {
				claims, err = serviceauth.ValidateScopedToken(tokenString, secret, policy.Caller, policy.Audience, policy.Purpose)
			} else {
				claims, err = serviceauth.ValidateToken(tokenString, secret, policy.Caller, policy.Audience)
			}
			if err != nil {
				return nil, errors.InvalidToken
			}

			ctx = context.WithValue(ctx, ContextKeyServiceName, claims.Service)
			ctx = context.WithValue(ctx, ContextKeyServicePurpose, claims.Purpose)
			return handler(ctx, req)
		}

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
		userId, err := auth.GetUserIdFromToken(tokenString, cfg.Secret)
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
		ctx = context.WithValue(ctx, ContextKeyUserId, userId)
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
