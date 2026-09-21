// Code scaffolded by goctl. Not to edit.

package health

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"

	"budgetmatch-sim/cmd/admin/internal/logic/health"
	"budgetmatch-sim/cmd/admin/internal/svc"
)

// 健康检查
func HealthHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx = r.Context()
		)

		l := health.NewHealthLogic(ctx, svcCtx)
		resp, err := l.Health()
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
