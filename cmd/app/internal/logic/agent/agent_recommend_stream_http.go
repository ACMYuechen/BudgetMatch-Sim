package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"budgetmatch-sim/cmd/app/internal/types"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// ServeHTTP owns SSE transport code; the generated handler delegates here.
func (l *AgentRecommendStreamLogic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := request.MustUserId(r.Context()); err != nil {
		writeStreamHTTPError(w, apperrors.Unauthorized)
		return
	}
	in := new(types.AgentRecommendStreamReq)
	// This endpoint has a small JSON request, independent of upload limits.
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := httpx.Parse(r, in); err != nil {
		writeStreamHTTPError(w, apperrors.Invalid)
		return
	}
	if l.svcCtx == nil || l.svcCtx.Validator == nil || l.svcCtx.AgentClient == nil {
		writeStreamHTTPError(w, apperrors.Internal)
		return
	}
	if err := l.svcCtx.Validator.Struct(in); err != nil || !validStreamRequest(&in.AgentRecommendReq) {
		writeStreamHTTPError(w, apperrors.Invalid)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		writeStreamHTTPError(w, apperrors.Internal)
		return
	}
	// go-zero's timeout middleware bypasses buffering for this exact Accept.
	// A v1 request without it must not silently run through a buffered writer.
	if in.StreamVersion == 1 && r.Header.Get("Accept") != "text/event-stream" {
		writeStreamHTTPError(w, apperrors.Invalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), streamcontract.MaxDuration)
	defer cancel()
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if in.StreamVersion == 1 {
		// Fail closed if middleware hides deadline support. A context alone
		// cannot interrupt an HTTP Write to a non-reading consumer.
		if err := controller.SetWriteDeadline(deadline); err != nil {
			writeStreamHTTPError(w, apperrors.Internal)
			return
		}
		// Keep it through net/http's final flush after this handler returns.
		// Clearing an expired deadline here could reintroduce an unbounded write.
	}
	contentType := "text/event-stream; charset=utf-8"
	if in.StreamVersion == 1 {
		contentType += "; version=1"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	written := 0
	emit := func(event StreamEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		payload, err := json.Marshal(event.Data)
		if err != nil || len(payload) > streamcontract.MaxEventBytes {
			cancel()
			return streamProtocolError()
		}
		frame := fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, payload)
		if event.ID != "" {
			frame = "id: " + event.ID + "\n" + frame
		}
		if len(frame) > streamcontract.MaxEventBytes || len(frame) > streamcontract.MaxHTTPBytes-written {
			cancel()
			return streamProtocolError()
		}
		written += len(frame)
		if n, err := w.Write([]byte(frame)); err != nil || n != len(frame) {
			cancel()
			if err == nil {
				return io.ErrShortWrite
			}
			return err
		}
		if err := controller.Flush(); err != nil {
			cancel()
			return err
		}
		return nil
	}
	runner := NewAgentRecommendStreamLogic(ctx, l.svcCtx)
	var err error
	if in.StreamVersion == 1 {
		err = runner.AgentRecommendStream(&in.AgentRecommendReq, emit)
	} else {
		err = runner.legacyRecommendStream(&in.AgentRecommendReq, emit)
	}
	if err != nil {
		// No raw error, request, model text, token or tool body in this log.
		logx.WithContext(ctx).Infow("recommendation HTTP stream stopped", logx.Field("stream_version", in.StreamVersion), logx.Field("bytes_sent", written))
	}
}

func writeStreamHTTPError(w http.ResponseWriter, err error) {
	code, body := apperrors.HTTPErrorHandler(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
