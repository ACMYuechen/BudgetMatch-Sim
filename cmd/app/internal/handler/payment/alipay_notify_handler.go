// Code scaffolded by goctl. Not to edit.

package payment

import (
	"net/http"

	"budgetmatch-sim/cmd/app/internal/logic/payment"
	"budgetmatch-sim/cmd/app/internal/svc"
)

// 支付宝异步通知
func AlipayNotifyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := payment.NewAlipayNotifyLogic(r.Context(), svcCtx)
		l.AlipayNotify(w, r)
	}
}
