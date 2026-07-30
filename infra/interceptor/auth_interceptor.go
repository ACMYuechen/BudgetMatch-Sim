// Package interceptor 提供通用的 gRPC 认证拦截器，供各 RPC 服务复用。
package interceptor

import (
	"context"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/infra/role"

	"google.golang.org/grpc"
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
//  1. 从 gRPC metadata 解析请求信息，白名单方法不执行身份校验
//  2. 受保护方法验证 JWT 签名并从 claims 提取 user_id、role
//  3. admin 方法要求 role.IsGlobalAdminRole，其余要求 role.IsGlobalUserRole
//  4. 将认证结果注入统一请求上下文，供业务读取和下游 RPC 传播
func UnaryServerInterceptor(cfg AuthConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// 1. 解析请求信息，白名单方法不执行身份校验
		ctx, tokenString, err := request.FromGRPCContext(ctx)
		if _, ok := cfg.NoAuthMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		// 2. 受保护方法必须携带合法格式的 Token
		if err != nil || tokenString == "" {
			return nil, errors.InvalidToken
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

		// 7. 注入可信身份信息，供业务读取和下游 RPC 透传
		ctx = request.WithUserID(ctx, userID)
		ctx = request.WithRole(ctx, int64(userRole))

		return handler(ctx, req)
	}
}
