// Code scaffolded by goctl. Not to edit.

package user

import (
	"github.com/zeromicro/go-zero/core/logx"

	"net/http"

	"budgetmatch-sim/cmd/app/internal/logic/user"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 获取当前用户信息
func UserInfoHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			in  = new(types.UserInfoReq)
			ctx = r.Context()
		)

		if err := httpx.Parse(r, in); err != nil {
			logx.WithContext(ctx).Errorf("parse params failed: %v", err)
			httpx.Error(w, err)
			return
		}

		if err := svcCtx.Validator.Struct(in); err != nil {
			logx.WithContext(ctx).Errorf("validate params failed: %v", err)
			httpx.Error(w, err)
			return
		}

		l := user.NewUserInfoLogic(ctx, svcCtx)
		resp, err := l.UserInfo(in)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
