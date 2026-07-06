// Code scaffolded by goctl. Not to edit.

package handler

import (
	"net/http"

	health "budgetmatch-sim/cmd/admin/internal/handler/health"
	mall_admin "budgetmatch-sim/cmd/admin/internal/handler/mall_admin"
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
					Handler: user.ListUsersHandler(serverCtx),
				},
				{
					// 按 ID 获取用户
					Method:  http.MethodGet,
					Path:    "/users/:user_id",
					Handler: user.GetUserByIdHandler(serverCtx),
				},
				{
					// 更新用户信息
					Method:  http.MethodPut,
					Path:    "/users/:user_id",
					Handler: user.UpdateUserInfoHandler(serverCtx),
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

	server.AddRoutes(
		rest.WithMiddlewares(
			[]rest.Middleware{serverCtx.AuthMiddleware},
			[]rest.Route{
				{
					// 创建商品
					Method:  http.MethodPost,
					Path:    "/products",
					Handler: mall_admin.AdminCreateProductHandler(serverCtx),
				},
				{
					// 更新商品
					Method:  http.MethodPut,
					Path:    "/products/:id",
					Handler: mall_admin.AdminUpdateProductHandler(serverCtx),
				},
				{
					// 删除商品
					Method:  http.MethodDelete,
					Path:    "/products/:id",
					Handler: mall_admin.AdminDeleteProductHandler(serverCtx),
				},
				{
					// 商品列表
					Method:  http.MethodGet,
					Path:    "/products",
					Handler: mall_admin.AdminProductListHandler(serverCtx),
				},
				{
					// 商品详情
					Method:  http.MethodGet,
					Path:    "/products/:id",
					Handler: mall_admin.AdminProductDetailHandler(serverCtx),
				},
				{
					// 创建SKU
					Method:  http.MethodPost,
					Path:    "/skus",
					Handler: mall_admin.AdminCreateSkuHandler(serverCtx),
				},
				{
					// 更新SKU
					Method:  http.MethodPut,
					Path:    "/skus/:id",
					Handler: mall_admin.AdminUpdateSkuHandler(serverCtx),
				},
				{
					// 删除SKU
					Method:  http.MethodDelete,
					Path:    "/skus/:id",
					Handler: mall_admin.AdminDeleteSkuHandler(serverCtx),
				},
				{
					// SKU列表
					Method:  http.MethodGet,
					Path:    "/skus",
					Handler: mall_admin.AdminSkuListHandler(serverCtx),
				},
				{
					// SKU详情
					Method:  http.MethodGet,
					Path:    "/skus/:id",
					Handler: mall_admin.AdminSkuDetailHandler(serverCtx),
				},
				{
					// 订单列表
					Method:  http.MethodGet,
					Path:    "/orders",
					Handler: mall_admin.AdminOrderListHandler(serverCtx),
				},
				{
					// 订单详情
					Method:  http.MethodGet,
					Path:    "/orders/:id",
					Handler: mall_admin.AdminOrderDetailHandler(serverCtx),
				},
				{
					// 更新订单状态
					Method:  http.MethodPut,
					Path:    "/orders/:id/status",
					Handler: mall_admin.AdminUpdateOrderStatusHandler(serverCtx),
				},
			}...,
		),
		rest.WithPrefix("/api/admin/mall"),
	)
}
