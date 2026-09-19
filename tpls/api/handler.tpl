{{if eq .HandlerName "AgentRecommendStreamHandler"}}// Code scaffolded by goctl. Not to edit.

package {{.PkgName}}

import (
	"net/http"

	"budgetmatch-sim/cmd/app/internal/logic/agent"
	"budgetmatch-sim/cmd/app/internal/svc"
)

func {{.HandlerName}}(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agent.NewAgentRecommendStreamLogic(r.Context(), svcCtx).ServeHTTP(w, r)
	}
}
{{else}}// Code scaffolded by goctl. Not to edit.

package {{.PkgName}}

import (
	{{if .HasRequest}}"github.com/zeromicro/go-zero/core/logx"
	{{end}}
	"net/http"
	{{if or (eq .HandlerName "AgentDemandPlanHandler") (eq .HandlerName "AgentDemandExecuteHandler")}}apperrors "budgetmatch-sim/infra/errors"{{end}}

	{{if ne .HandlerName "AlipayNotifyHandler"}}"github.com/zeromicro/go-zero/rest/httpx"
	{{end}}
	{{.ImportPackages}}
)

{{if .HasDoc}}{{.Doc}}{{end}}
func {{.HandlerName}}(svcCtx *svc.ServiceContext) http.HandlerFunc {
	{{if eq .HandlerName "AlipayNotifyHandler"}}return func(w http.ResponseWriter, r *http.Request) {
		l := {{.LogicName}}.New{{.LogicType}}(r.Context(), svcCtx)
		l.{{.Call}}(w, r)
	}
	{{else}}
	return func(w http.ResponseWriter, r *http.Request) {
		var (
      	{{if .HasRequest}}in = new(types.{{.RequestType}}){{end}}
			ctx  = r.Context()
		)

		{{if .HasRequest}}if err := httpx.Parse(r, in); err != nil {
			{{if or (eq .HandlerName "AgentDemandPlanHandler") (eq .HandlerName "AgentDemandExecuteHandler")}}logx.WithContext(ctx).Error("invalid demand planning request")
			httpx.Error(w, apperrors.Invalid)
			{{else}}
			logx.WithContext(ctx).Errorf("parse params failed: %v", err)
			httpx.Error(w, err)
			{{end}}
			return
		}{{end}}

		
		{{if .HasRequest}}if err := svcCtx.Validator.Struct(in); err != nil {
			{{if or (eq .HandlerName "AgentDemandPlanHandler") (eq .HandlerName "AgentDemandExecuteHandler")}}logx.WithContext(ctx).Error("invalid demand planning parameters")
			httpx.Error(w, apperrors.Invalid)
			{{else}}
			logx.WithContext(ctx).Errorf("validate params failed: %v", err)
			httpx.Error(w, err)
			{{end}}
			return
		}{{end}}

		l := {{.LogicName}}.New{{.LogicType}}(ctx, svcCtx)
		{{if .HasResp}}resp, {{end}}err := l.{{.Call}}({{if .HasRequest}}in{{end}})
		if err != nil {
			httpx.Error(w, err)
		} else {
			{{if .HasResp}}httpx.OkJson(w, resp){{else}}httpx.Ok(w){{end}}
		}
	}
{{end}}}
{{end}}
