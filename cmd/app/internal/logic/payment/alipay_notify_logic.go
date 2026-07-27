package payment

import (
	"context"
	"net/http"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/services/rpc/payment/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// alipayNotifySuccess 是支付宝确认通知已成功消费的固定响应。
	alipayNotifySuccess = "success"
	// alipayNotifyFailure 表示处理失败，支付宝会按协议重试通知。
	alipayNotifyFailure = "failure"
)

// AlipayNotifyLogic 负责处理网关收到的支付宝异步通知。
type AlipayNotifyLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

// NewAlipayNotifyLogic 创建支付宝通知处理逻辑。
func NewAlipayNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AlipayNotifyLogic {
	return &AlipayNotifyLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// AlipayNotify 读取 POST 表单并转发给 payment-rpc 验签和确认支付。
// 网关不持有支付宝密钥，处理结果仅按协议回写 success 或 failure。
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

// writeAlipayNotifyResponse 按支付宝协议返回纯文本确认结果。
func writeAlipayNotifyResponse(w http.ResponseWriter, success bool) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if success {
		_, _ = w.Write([]byte(alipayNotifySuccess))
		return
	}
	_, _ = w.Write([]byte(alipayNotifyFailure))
}
