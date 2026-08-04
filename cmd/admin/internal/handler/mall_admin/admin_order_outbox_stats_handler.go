// Code scaffolded by goctl. Not to edit.

package mall_admin

import (
	"net/http"

	"budgetmatch-sim/cmd/admin/internal/logic/mall_admin"
	"budgetmatch-sim/cmd/admin/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 订单Outbox状态统计
func AdminOrderOutboxStatsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx = r.Context()
		)

		l := mall_admin.NewAdminOrderOutboxStatsLogic(ctx, svcCtx)
		resp, err := l.AdminOrderOutboxStats()
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
