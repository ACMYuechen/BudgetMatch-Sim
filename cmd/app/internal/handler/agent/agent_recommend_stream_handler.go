// Code scaffolded by goctl. Not to edit.

package agent

import (
	"net/http"

	"budgetmatch-sim/cmd/app/internal/logic/agent"
	"budgetmatch-sim/cmd/app/internal/svc"
)

func AgentRecommendStreamHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agent.NewAgentRecommendStreamLogic(r.Context(), svcCtx).ServeHTTP(w, r)
	}
}
