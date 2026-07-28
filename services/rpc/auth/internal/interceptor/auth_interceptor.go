package interceptor

import (
	"context"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/auth/internal/svc"

	"google.golang.org/grpc"
)

var noAuthMethods = map[string]struct{}{
	"/auth.AuthService/UsernameLogin": {},
	"/auth.AuthService/EmailLogin":    {},
	"/auth.AuthService/EmailRegister": {},
	"/auth.AuthService/ValidateToken": {},
	"/auth.AuthService/SendCode":      {},
	"/auth.AuthService/LoginByCode":   {},
}

// AuthInterceptor 从统一请求上下文中解析 Token，验证后注入可信身份信息。
func AuthInterceptor(svcCtx *svc.ServiceContext) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 不需要认证的接口白名单
		if _, ok := noAuthMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		// 统一解析 gRPC metadata 中的请求信息
		requestInfo, err := request.FromGRPCContext(ctx)
		if err != nil || requestInfo.Token == "" {
			return nil, errors.InvalidToken
		}
		tokenString := requestInfo.Token

		// 验证 token
		_, err = auth.ValidateToken(tokenString, svcCtx.Config.JwtAuth.Secret)
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
		if !role.IsGlobalUserRole(int64(u.Role)) {
			return nil, errors.Unauthorized
		}

		// 注入可信身份摘要，业务层统一通过 request 包读取
		requestInfo.UserID = u.Id
		requestInfo.Role = int64(u.Role)
		ctx = request.NewContext(ctx, requestInfo)

		return handler(ctx, req)
	}
}
