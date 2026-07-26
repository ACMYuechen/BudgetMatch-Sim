package payment

import (
	"context"
	"net/http"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/services/rpc/payment/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	alipayNotifySuccess = "success"
	alipayNotifyFailure = "failure"
)

type AlipayNotifyLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAlipayNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AlipayNotifyLogic {
	return &AlipayNotifyLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *AlipayNotifyLogic) AlipayNotify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		l.Logger.Errorf("parse alipay notify form failed: %v", err)
		writeAlipayNotifyResponse(w, false)
		return
	}

	params := make(map[string]string, len(r.PostForm))
	for key, values := range r.PostForm {
		if len(values) > 0 {
			params[key] = values[0]
		} else {
			params[key] = ""
		}
	}

	resp, err := l.svcCtx.PaymentClient.HandleNotify(l.ctx, &pb.HandleNotifyReq{Params: params})
	if err != nil {
		l.Logger.Errorf("handle alipay notify failed: %v", err)
		writeAlipayNotifyResponse(w, false)
		return
	}
	if resp == nil {
		l.Logger.Error("payment service returned an empty notify response")
		writeAlipayNotifyResponse(w, false)
		return
	}
	if !resp.Ok {
		l.Logger.Errorf("payment service rejected alipay notify: %s", resp.Message)
		writeAlipayNotifyResponse(w, false)
		return
	}

	writeAlipayNotifyResponse(w, true)
}

func writeAlipayNotifyResponse(w http.ResponseWriter, success bool) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if success {
		_, _ = w.Write([]byte(alipayNotifySuccess))
		return
	}
	_, _ = w.Write([]byte(alipayNotifyFailure))
}
