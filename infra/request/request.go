// Package request 提供统一的请求上下文解析、注入和读取能力。
//
// 本文件为各 ctx 字段提供单独的 With*（注入）、Try*（可选读取）和 Must*（必须读取）方法。
// Request 结构体仅作为 FromHTTPRequest / FromGRPCContext 的返回值载体，不参与 Context 存取。
package request

import (
	"context"
	"fmt"
	"net/textproto"
	"strings"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/trace"

	"budgetmatch-sim/infra/errors"
)

// ────────────────────────────── context key ──────────────────────────────

type ctxKey string

const (
	ctxKeyToken     ctxKey = "token"
	ctxKeyUserID    ctxKey = "user_id"
	ctxKeyRole      ctxKey = "role"
	ctxKeyRequestID ctxKey = "request_id"
	ctxKeyClientIP  ctxKey = "client_ip"
	ctxKeyUserAgent ctxKey = "user_agent"
	ctxKeyHeaders   ctxKey = "headers"
)

// ──────────────────────── 公开常量（供 grpc.go / http.go 使用）─────────────

const (
	// HeaderAuthorization 是认证信息请求头。
	HeaderAuthorization = "Authorization"
	// HeaderRequestID 是请求链路标识请求头。
	HeaderRequestID = "X-Request-Id"
	// HeaderUserAgent 是客户端标识请求头。
	HeaderUserAgent = "User-Agent"
)

var allowedHeaders = map[string]struct{}{
	HeaderAuthorization: {},
	HeaderRequestID:     {},
	HeaderUserAgent:     {},
}

// ──────────────────────── Request 结构体 ────────────────────────────────

// Request 保存一次请求中需要跨层传递的业务信息。
type Request struct {
	Token     string
	UserID    string
	Role      int64
	RequestID string
	ClientIP  string
	UserAgent string
	Headers   map[string]string
}

// ──────────────────────── Token ──────────────────────────────────────────

// WithToken 将 JWT Token 注入 Context，同时写入 go-zero 日志字段。
func WithToken(ctx context.Context, token string) context.Context {
	return logx.ContextWithFields(
		context.WithValue(ctx, ctxKeyToken, token),
		logx.Field(string(ctxKeyToken), maskToken(token)),
	)
}

// TryToken 返回 Context 中的 Token，缺失时返回空字符串。
func TryToken(ctx context.Context) string {
	tk, _ := ctx.Value(ctxKeyToken).(string)
	return tk
}

// MustToken 返回 Context 中的 Token，缺失时返回错误。
func MustToken(ctx context.Context) (string, error) {
	tk := TryToken(ctx)
	if tk == "" {
		return "", errors.InvalidToken
	}
	return tk, nil
}

// ──────────────────────── UserID ────────────────────────────────────────

// WithUserID 将用户 ID 注入 Context。
func WithUserID(ctx context.Context, userID string) context.Context {
	return logx.ContextWithFields(
		context.WithValue(ctx, ctxKeyUserID, userID),
		logx.Field(string(ctxKeyUserID), userID),
	)
}

// TryUserID 返回 Context 中的用户 ID，缺失时返回空字符串。
func TryUserID(ctx context.Context) string {
	uid, _ := ctx.Value(ctxKeyUserID).(string)
	return uid
}

// MustUserID 返回 Context 中的用户 ID，缺失时返回错误。
func MustUserID(ctx context.Context) (string, error) {
	uid := TryUserID(ctx)
	if uid == "" {
		return "", errors.RequestUserIDRequired
	}
	return uid, nil
}

// ──────────────────────── Role ──────────────────────────────────────────

// WithRole 将用户角色注入 Context。
func WithRole(ctx context.Context, role int64) context.Context {
	return logx.ContextWithFields(
		context.WithValue(ctx, ctxKeyRole, role),
		logx.Field(string(ctxKeyRole), fmt.Sprintf("%d", role)),
	)
}

// TryRole 返回 Context 中的用户角色，缺失时返回 0。
func TryRole(ctx context.Context) int64 {
	role, _ := ctx.Value(ctxKeyRole).(int64)
	return role
}

// MustRole 返回 Context 中的用户角色，缺失时返回错误。
func MustRole(ctx context.Context) (int64, error) {
	role := TryRole(ctx)
	if role == 0 {
		return 0, errors.Unauthorized
	}
	return role, nil
}

// ──────────────────────── RequestID ─────────────────────────────────────

// WithRequestID 将请求 ID 注入 Context，同时写入 go-zero 日志字段。
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return logx.ContextWithFields(
		context.WithValue(ctx, ctxKeyRequestID, requestID),
		logx.Field(string(ctxKeyRequestID), requestID),
	)
}

// TracedRequestID 取 traceID-spanID 作为每次调用唯一的 request_id。
//
// span 由服务端生成，客户端复用 traceparent 也不会撞键；
// trace 未启用（全 0）时退回随机 uuid 避免塌缩。
func TracedRequestID(ctx context.Context) string {
	tid, sid := trace.TraceIDFromContext(ctx), trace.SpanIDFromContext(ctx)
	if isZeroHex(tid) || isZeroHex(sid) {
		return strings.ReplaceAll(uuid.New().String(), "-", "")
	}
	return tid + "-" + sid
}

func isZeroHex(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

// TryRequestID 返回 Context 中的请求 ID，缺失时返回空字符串。
func TryRequestID(ctx context.Context) string {
	rid, _ := ctx.Value(ctxKeyRequestID).(string)
	return rid
}

// ──────────────────────── ClientIP ──────────────────────────────────────

// WithClientIP 将客户端 IP 注入 Context。
func WithClientIP(ctx context.Context, ip string) context.Context {
	return logx.ContextWithFields(
		context.WithValue(ctx, ctxKeyClientIP, ip),
		logx.Field(string(ctxKeyClientIP), ip),
	)
}

// TryClientIP 返回 Context 中的客户端 IP，缺失时返回空字符串。
func TryClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(ctxKeyClientIP).(string)
	return ip
}

// ──────────────────────── UserAgent ─────────────────────────────────────

// WithUserAgent 将 User-Agent 注入 Context。
func WithUserAgent(ctx context.Context, ua string) context.Context {
	return logx.ContextWithFields(
		context.WithValue(ctx, ctxKeyUserAgent, ua),
		logx.Field(string(ctxKeyUserAgent), ua),
	)
}

// TryUserAgent 返回 Context 中的 User-Agent，缺失时返回空字符串。
func TryUserAgent(ctx context.Context) string {
	ua, _ := ctx.Value(ctxKeyUserAgent).(string)
	return ua
}

// ──────────────────────── Headers ───────────────────────────────────────

// WithHeaders 将白名单请求头注入 Context。
func WithHeaders(ctx context.Context, headers map[string]string) context.Context {
	return context.WithValue(ctx, ctxKeyHeaders, cloneAllowedHeaders(headers))
}

// TryHeaders 返回 Context 中的白名单请求头，缺失时返回 nil。
func TryHeaders(ctx context.Context) map[string]string {
	h, _ := ctx.Value(ctxKeyHeaders).(map[string]string)
	return h
}

// TryHeader 返回白名单内的单个请求头，名称匹配不区分大小写。
func TryHeader(ctx context.Context, key string) string {
	headers := TryHeaders(ctx)
	if headers == nil {
		return ""
	}
	return headers[canonicalHeader(key)]
}

// ──────────────────────── 内部辅助函数 ───────────────────────────────────

func cloneAllowedHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		key = canonicalHeader(key)
		if _, ok := allowedHeaders[key]; !ok {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			cloned[key] = value
		}
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func canonicalHeader(key string) string {
	return textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(key))
}

// maskToken 截断 Token 仅保留前 8 位用于日志，避免泄露完整凭据。
func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return token[:1] + "***"
	}
	return token[:8] + "***"
}
