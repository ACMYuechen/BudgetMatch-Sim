// Code scaffolded by goctl. Not to edit.

package handler

import (
	"net/http"{{if .hasTimeout}}
	"time"{{end}}

	{{.importPackages}}
)

func RegisterHandlers(server *rest.Server, serverCtx *svc.ServiceContext) {
	{{.routesAdditions}}
}
