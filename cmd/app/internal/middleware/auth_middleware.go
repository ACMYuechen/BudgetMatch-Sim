package middleware

import (
	"context"
	"net/http"
	"strings"

	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/auth/client/authservice"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuthMiddleware struct {
	authRpc authservice.AuthService
}

func NewAuthMiddleware(authRpc authservice.AuthService) *AuthMiddleware {
	return &AuthMiddleware{authRpc: authRpc}
}

func (m *AuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// 解析 Token
		tokenString := r.Header.Get("Authorization")
		if tokenString == "" {
			logx.WithContext(ctx).Error("missing Authorization header")
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return
		}

		// 去掉 Bearer 前缀
		if len(tokenString) > 7 && strings.HasPrefix(tokenString, "Bearer ") {
			tokenString = tokenString[7:]
		}

		// 通过 RPC 验证 token
		resp, err := m.authRpc.ValidateToken(ctx, &pb.ValidateTokenReq{Token: tokenString})
		if err != nil {
			logx.WithContext(ctx).Errorf("invalid token: %v", err)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if resp.User == nil || resp.User.Id == "" {
			logx.WithContext(ctx).Error("invalid token response")
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		// 检查是否为全局用户身份（含管理员和普通用户，拒绝未注册角色）
		if !role.IsGlobalUserRole(int64(resp.User.Role)) {
			logx.WithContext(ctx).Errorf("unauthorized: role=%d", resp.User.Role)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// 将 user_id、token 和完整用户信息注入 context
		ctx = context.WithValue(ctx, "user_id", resp.User.Id)
		ctx = context.WithValue(ctx, "token", tokenString)
		ctx = context.WithValue(ctx, "user", resp.User)

		next(w, r.WithContext(ctx))
	}
}
