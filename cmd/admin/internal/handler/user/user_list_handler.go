// Code scaffolded by goctl. Safe to edit.

package user

import (
	"fmt"
	"net/http"

	"budgetmatch-sim/cmd/admin/internal/logic/user"
	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// 用户列表
func UserListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.UserListReq
		req.Page = 1
		req.PageSize = 10
		if p := r.URL.Query().Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &req.Page)
		}
		if ps := r.URL.Query().Get("page_size"); ps != "" {
			fmt.Sscanf(ps, "%d", &req.PageSize)
		}
		if s := r.URL.Query().Get("status"); s != "" {
			fmt.Sscanf(s, "%d", &req.Status)
		}
		if ro := r.URL.Query().Get("role"); ro != "" {
			fmt.Sscanf(ro, "%d", &req.Role)
		}

		l := user.NewUserListLogic(r.Context(), svcCtx)
		resp, err := l.UserList(&req)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
