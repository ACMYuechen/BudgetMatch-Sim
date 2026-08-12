// Code scaffolded by goctl. Not to edit.

package agent

import (
	"github.com/zeromicro/go-zero/core/logx"

	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"

	"budgetmatch-sim/cmd/app/internal/logic/agent"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
)

// Agent 会话列表
func AgentConversationListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		var (
			in  = new(types.AgentConversationListReq)
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

		l := agent.NewAgentConversationListLogic(ctx, svcCtx)
		resp, err := l.AgentConversationList(in)
		if err != nil {
			httpx.Error(w, err)
		} else {
			httpx.OkJson(w, resp)
		}
	}
}
