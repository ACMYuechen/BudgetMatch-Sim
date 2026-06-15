// Code scaffolded by goctl. Not to edit.

package mall_admin

import (
	"net/http"

	"budgetmatch-sim/cmd/admin/internal/logic/mall_admin"
	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 删除SKU
func AdminDeleteSkuHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AdminDeleteSkuReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.Error(w, err)
			return
		}

		l := mall_admin.NewAdminDeleteSkuLogic(r.Context(), svcCtx)
		err := l.AdminDeleteSku(&req)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.Ok(w)
		}
	}
}
