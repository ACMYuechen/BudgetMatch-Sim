// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package health

import (
	"net/http"

	"budgetmatch-sim/cmd/admin/internal/logic/health"
	"budgetmatch-sim/cmd/admin/internal/svc"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// 健康检查
func HealthHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx = r.Context()
		)

		l := health.NewHealthLogic(ctx, svcCtx)
		err := l.Health()
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, "OK")
		}
	}
}
