// Code scaffolded by goctl. Not to edit.

package handler

import (
	"net/http"

	health "budgetmatch-sim/cmd/app/internal/handler/health"
	mall "budgetmatch-sim/cmd/app/internal/handler/mall"
	seckill "budgetmatch-sim/cmd/app/internal/handler/seckill"
	"budgetmatch-sim/cmd/app/internal/svc"

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
			[]rest.Middleware{serverCtx.AuthMiddleware, serverCtx.SeckillRateLimitMiddleware},
			[]rest.Route{
				{
					// 活动列表
					Method:  http.MethodGet,
					Path:    "/activities",
					Handler: seckill.ActivityListHandler(serverCtx),
				},
				{
					// 活动详情
					Method:  http.MethodGet,
					Path:    "/activities/:id",
					Handler: seckill.ActivityDetailHandler(serverCtx),
				},
				{
					// SKU列表
					Method:  http.MethodGet,
					Path:    "/skus",
					Handler: seckill.SkuListHandler(serverCtx),
				},
				{
					// 获取秒杀令牌
					Method:  http.MethodPost,
					Path:    "/token",
					Handler: seckill.AcquireTokenHandler(serverCtx),
				},
				{
					// 提交秒杀订单
					Method:  http.MethodPost,
					Path:    "/orders",
					Handler: seckill.SubmitOrderHandler(serverCtx),
				},
				{
					// 查询订单
					Method:  http.MethodGet,
					Path:    "/orders/:order_id",
					Handler: seckill.GetOrderHandler(serverCtx),
				},
			}...,
		),
		rest.WithPrefix("/api/seckill"),
	)

	server.AddRoutes(
		rest.WithMiddlewares(
			[]rest.Middleware{serverCtx.AuthMiddleware},
			[]rest.Route{
				{
					// 商品列表
					Method:  http.MethodGet,
					Path:    "/products",
					Handler: mall.ProductListHandler(serverCtx),
				},
				{
					// 商品详情
					Method:  http.MethodGet,
					Path:    "/products/:id",
					Handler: mall.ProductDetailHandler(serverCtx),
				},
				{
					// SKU列表
					Method:  http.MethodGet,
					Path:    "/skus",
					Handler: mall.SkuListHandler(serverCtx),
				},
				{
					// 创建订单
					Method:  http.MethodPost,
					Path:    "/orders",
					Handler: mall.CreateOrderHandler(serverCtx),
				},
				{
					// 订单列表
					Method:  http.MethodGet,
					Path:    "/orders",
					Handler: mall.OrderListHandler(serverCtx),
				},
				{
					// 订单详情
					Method:  http.MethodGet,
					Path:    "/orders/:id",
					Handler: mall.OrderDetailHandler(serverCtx),
				},
				{
					// 取消订单
					Method:  http.MethodPost,
					Path:    "/orders/:id/cancel",
					Handler: mall.CancelOrderHandler(serverCtx),
				},
			}...,
		),
		rest.WithPrefix("/api/mall"),
	)
}
