// Code scaffolded by goctl. Not to edit.

package handler

import (
	"net/http"

	health "budgetmatch-sim/cmd/admin/internal/handler/health"
	user "budgetmatch-sim/cmd/admin/internal/handler/user"
	"budgetmatch-sim/cmd/admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

func RegisterHandlers(server *rest.Server, serverCtx *svc.ServiceContext) {
	server.AddRoutes(
		[]rest.Route{
			{
				// 健康检查
				Method:  http.MethodGet,
				Path:    "/health",
				Handler: health.HealthHandler(serverCtx),
			},
		},
		rest.WithPrefix("/api"),
	)

	server.AddRoutes(
		rest.WithMiddlewares(
			[]rest.Middleware{serverCtx.AuthMiddleware},
			[]rest.Route{
				{
					// 用户列表
					Method:  http.MethodGet,
					Path:    "/users",
					Handler: user.UserListHandler(serverCtx),
				},
				{
					// 用户详情
					Method:  http.MethodGet,
					Path:    "/users/:user_id",
					Handler: user.GetUserDetailHandler(serverCtx),
				},
				{
					// 更新用户状态
					Method:  http.MethodPut,
					Path:    "/users/status",
					Handler: user.UpdateUserStatusHandler(serverCtx),
				},
				{
					// 删除用户
					Method:  http.MethodDelete,
					Path:    "/users/:user_id",
					Handler: user.DeleteUserHandler(serverCtx),
				},
			}...,
		),
		rest.WithPrefix("/api/admin"),
	)
}
