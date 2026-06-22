// Code scaffolded by goctl. Not to edit.

package agent

import (
	"encoding/json"
	"fmt"
	"net/http"

	agentlogic "budgetmatch-sim/cmd/app/internal/logic/agent"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AgentRecommendStreamHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			in  = new(types.AgentRecommendReq)
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

		flusher, ok := w.(http.Flusher)
		if !ok {
			httpx.Error(w, fmt.Errorf("streaming is not supported"))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		l := agentlogic.NewAgentRecommendStreamLogic(ctx, svcCtx)
		err := l.AgentRecommendStream(in, func(event agentlogic.StreamEvent) error {
			return writeSSE(w, flusher, event)
		})
		if err != nil {
			logx.WithContext(ctx).Errorf("stream recommend failed: %v", err)
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event agentlogic.StreamEvent) error {
	data, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
