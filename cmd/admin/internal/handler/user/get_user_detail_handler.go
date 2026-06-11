// Code scaffolded by goctl. Safe to edit.

package user

import (
	"net/http"

	"budgetmatch-sim/cmd/admin/internal/logic/user"
	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// 用户详情
func GetUserDetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetUserDetailReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.Error(w, err)
			return
		}

		l := user.NewGetUserDetailLogic(r.Context(), svcCtx)
		resp, err := l.GetUserDetail(&req)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
