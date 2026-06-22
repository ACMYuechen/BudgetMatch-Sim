// Code scaffolded by goctl. Not to edit.

package auth

import (
	"github.com/zeromicro/go-zero/core/logx"

	"net/http"

	"budgetmatch-sim/cmd/app/internal/logic/auth"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 验证码登录
func LoginByCodeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			in  = new(types.LoginByCodeReq)
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

		l := auth.NewLoginByCodeLogic(ctx, svcCtx)
		resp, err := l.LoginByCode(in)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
