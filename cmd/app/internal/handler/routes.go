// Code scaffolded by goctl. Not to edit.

package handler

import (
	"net/http"

	health "budgetmatch-sim/cmd/app/internal/handler/health"
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
}
