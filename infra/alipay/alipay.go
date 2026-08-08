// Package alipay 封装支付宝（沙箱/生产）当面付能力，供 payment-rpc 使用。
// 设计风格参照 infra/oss：Config + 构造函数 + 少量业务方法。
package alipay

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/smartwalle/alipay/v3"
)

// Config 支付宝配置。
//
// 沙箱与生产共用同一份字段，仅通过 IsProduction 区分网关：
//   - IsProduction = false → 沙箱网关 openapi.alipaydev.com
//   - IsProduction = true  → 正式网关 openapi.alipay.com
//
// 密钥均为「密钥模式」下的 PKCS1/PKCS8 字符串（不含 -----BEGIN----- 头亦可）。
type Config struct {
	AppId           string `json:",optional"` // 应用 AppId
	PrivateKey      string `json:",optional"` // 应用私钥
	AlipayPublicKey string `json:",optional"` // 支付宝公钥（用于验签）
	IsProduction    bool   `json:",default=false"`
	NotifyURL       string `json:",optional"` // 异步通知地址（公网可达的网关地址）
	ReturnURL       string `json:",optional"` // 同步跳转地址
}

// Enabled 表示是否已填入必要密钥。未配置时 payment-rpc 仍可启动，
// 但发起支付会返回未配置错误，方便先把结构跑起来、密钥后补。
func (c Config) Enabled() bool {
	return c.AppId != "" && c.PrivateKey != "" && c.AlipayPublicKey != ""
}

// Client 支付宝客户端封装。
type Client struct {
	cfg    Config
	client *alipay.Client
}

// PreCreateResult 预下单（当面付扫码）结果。
type PreCreateResult struct {
	OutTradeNo string
	QrCode     string // 二维码码串，前端据此渲染二维码图片
}

// QueryResult 交易查询结果。
type QueryResult struct {
	TradeStatus string // 原始交易状态：WAIT_BUYER_PAY / TRADE_SUCCESS / TRADE_FINISHED / TRADE_CLOSED
	TradeNo     string
	OutTradeNo  string
	BuyerId     string
	BuyerLogon  string
	Paid        bool // TRADE_SUCCESS 或 TRADE_FINISHED
}

// NewClient 构造支付宝客户端。未配置密钥时返回 (nil, nil)，由调用方决定降级行为。
func NewClient(cfg Config) (*Client, error) {
	if !cfg.Enabled() {
		return nil, nil
	}

	c, err := alipay.New(cfg.AppId, cfg.PrivateKey, cfg.IsProduction)
	if err != nil {
		return nil, fmt.Errorf("alipay: init client failed: %w", err)
	}
	if err := c.LoadAliPayPublicKey(cfg.AlipayPublicKey); err != nil {
		return nil, fmt.Errorf("alipay: load public key failed: %w", err)
	}

	return &Client{cfg: cfg, client: c}, nil
}

// PreCreate 统一收单线下交易预创建（扫码支付），返回二维码码串。
// amountFen 单位为分，内部转换为支付宝要求的「元」字符串。
func (c *Client) PreCreate(ctx context.Context, outTradeNo, subject string, amountFen int64) (*PreCreateResult, error) {
	var p = alipay.TradePreCreate{}
	p.OutTradeNo = outTradeNo
	p.Subject = subject
	p.TotalAmount = fenToYuan(amountFen)
	p.NotifyURL = c.cfg.NotifyURL

	rsp, err := c.client.TradePreCreate(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("alipay: precreate failed: %w", err)
	}
	if rsp.Code != alipay.CodeSuccess {
		return nil, fmt.Errorf("alipay: precreate biz error: %s-%s", rsp.Code, rsp.Msg)
	}

	return &PreCreateResult{
		OutTradeNo: rsp.OutTradeNo,
		QrCode:     rsp.QRCode,
	}, nil
}

// Query 交易查询，outTradeNo 与 tradeNo 二选一。
func (c *Client) Query(ctx context.Context, outTradeNo, tradeNo string) (*QueryResult, error) {
	var p = alipay.TradeQuery{
		OutTradeNo: outTradeNo,
		TradeNo:    tradeNo,
	}
	rsp, err := c.client.TradeQuery(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("alipay: query failed: %w", err)
	}
	if rsp.Code != alipay.CodeSuccess {

		// 预下单后、用户扫码前，支付宝可能查不到实际交易。
		// 对本系统来说仍然是待支付状态。
		if rsp.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return &QueryResult{
				OutTradeNo: outTradeNo,
				Paid:       false,
			}, nil
		}

		return nil, fmt.Errorf("alipay: query biz error: %s-%s", rsp.Code, rsp.Msg)
	}

	status := string(rsp.TradeStatus)
	return &QueryResult{
		TradeStatus: status,
		TradeNo:     rsp.TradeNo,
		OutTradeNo:  rsp.OutTradeNo,
		BuyerId:     rsp.BuyerUserId,
		BuyerLogon:  rsp.BuyerLogonId,
		Paid:        status == string(alipay.TradeStatusSuccess) || status == string(alipay.TradeStatusFinished),
	}, nil
}

// DecodeNotify 验签并解析异步通知参数。验签失败返回 error。
func (c *Client) DecodeNotify(ctx context.Context, params map[string]string) (*alipay.Notification, error) {
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	noti, err := c.client.DecodeNotification(ctx, values)
	if err != nil {
		return nil, fmt.Errorf("alipay: decode notify failed: %w", err)
	}
	return noti, nil
}

// fenToYuan 分转元字符串，保留两位小数。
func fenToYuan(fen int64) string {
	return strconv.FormatFloat(float64(fen)/100.0, 'f', 2, 64)
}
