// Code scaffolded by goctl. Not to edit.

package mall

import (
	"github.com/zeromicro/go-zero/core/logx"

	"net/http"

	"budgetmatch-sim/cmd/app/internal/logic/mall"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 发起支付宝扫码支付
func CreatePaymentHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			in  = new(types.MallCreatePaymentReq)
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

		l := mall.NewCreatePaymentLogic(ctx, svcCtx)
		resp, err := l.CreatePayment(in)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
