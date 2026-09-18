// Code scaffolded by goctl. Not to edit.

package agent

import (
	"github.com/zeromicro/go-zero/core/logx"

	apperrors "budgetmatch-sim/infra/errors"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"

	"budgetmatch-sim/cmd/app/internal/logic/agent"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
)

// 执行已确认需求（显式演示模式，非实时商城库存）
func AgentDemandExecuteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		var (
			in  = new(types.AgentDemandExecuteReq)
			ctx = r.Context()
		)

		if err := httpx.Parse(r, in); err != nil {
			logx.WithContext(ctx).Error("invalid demand planning request")
			httpx.Error(w, apperrors.Invalid)

			return
		}

		if err := svcCtx.Validator.Struct(in); err != nil {
			logx.WithContext(ctx).Error("invalid demand planning parameters")
			httpx.Error(w, apperrors.Invalid)

			return
		}

		l := agent.NewAgentDemandExecuteLogic(ctx, svcCtx)
		resp, err := l.AgentDemandExecute(in)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
