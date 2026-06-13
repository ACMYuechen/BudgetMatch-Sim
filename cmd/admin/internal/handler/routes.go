// Code scaffolded by goctl. Not to edit.

package handler

import (
	"net/http"

	health "budgetmatch-sim/cmd/admin/internal/handler/health"
	seckill "budgetmatch-sim/cmd/admin/internal/handler/seckill"
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

	server.AddRoutes(
		rest.WithMiddlewares(
			[]rest.Middleware{serverCtx.AuthMiddleware},
			[]rest.Route{
				{
					// 活动列表
					Method:  http.MethodGet,
					Path:    "/activities",
					Handler: seckill.ActivityListHandler(serverCtx),
				},
				{
					// 创建活动
					Method:  http.MethodPost,
					Path:    "/activities",
					Handler: seckill.ActivityCreateHandler(serverCtx),
				},
				{
					// 活动详情
					Method:  http.MethodGet,
					Path:    "/activities/:id",
					Handler: seckill.ActivityDetailHandler(serverCtx),
				},
				{
					// 更新活动
					Method:  http.MethodPut,
					Path:    "/activities/:id",
					Handler: seckill.ActivityUpdateHandler(serverCtx),
				},
				{
					// 删除活动
					Method:  http.MethodDelete,
					Path:    "/activities/:id",
					Handler: seckill.ActivityDeleteHandler(serverCtx),
				},
				{
					// 预热活动
					Method:  http.MethodPost,
					Path:    "/activities/:id/preheat",
					Handler: seckill.ActivityPreheatHandler(serverCtx),
				},
				{
					// 上线活动
					Method:  http.MethodPost,
					Path:    "/activities/:id/online",
					Handler: seckill.ActivityOnlineHandler(serverCtx),
				},
				{
					// 下线活动
					Method:  http.MethodPost,
					Path:    "/activities/:id/offline",
					Handler: seckill.ActivityOfflineHandler(serverCtx),
				},
				{
					// SKU列表
					Method:  http.MethodGet,
					Path:    "/skus",
					Handler: seckill.SkuListHandler(serverCtx),
				},
				{
					// 创建SKU
					Method:  http.MethodPost,
					Path:    "/skus",
					Handler: seckill.SkuCreateHandler(serverCtx),
				},
				{
					// SKU详情
					Method:  http.MethodGet,
					Path:    "/skus/:id",
					Handler: seckill.SkuDetailHandler(serverCtx),
				},
				{
					// 更新SKU
					Method:  http.MethodPut,
					Path:    "/skus/:id",
					Handler: seckill.SkuUpdateHandler(serverCtx),
				},
				{
					// 删除SKU
					Method:  http.MethodDelete,
					Path:    "/skus/:id",
					Handler: seckill.SkuDeleteHandler(serverCtx),
				},
			}...,
		),
		rest.WithPrefix("/api/admin/seckill"),
	)
}
