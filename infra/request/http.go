package request

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"budgetmatch-sim/infra/errors"
)

const (
	headerForwardedFor = "X-Forwarded-For"
	headerRealIP       = "X-Real-Ip"
)

// FromHTTPRequest 从 HTTP 请求中解析业务请求信息并注入 Context。
//
// 返回注入后的 Context、Token 和可能的错误。
// 该函数只负责提取 Token，不校验 Token 签名和 claims。
func FromHTTPRequest(ctx context.Context, r *http.Request) (context.Context, string, error) {
	if r == nil {
		return ctx, "", errors.RequestNilHTTP
	}

	authorization, err := singleHeader(r.Header, HeaderAuthorization)
	if err != nil {
		return ctx, "", err
	}
	token, err := parseAuthorization(authorization)
	if err != nil {
		return ctx, "", err
	}

	requestID := strings.TrimSpace(r.Header.Get(HeaderRequestID))

	headers := make(map[string]string, len(allowedHeaders))
	for key := range allowedHeaders {
		value := strings.TrimSpace(r.Header.Get(key))
		if value != "" {
			headers[key] = value
		}
	}

	ctx = WithToken(ctx, token)
	ctx = WithRequestID(ctx, requestID)
	ctx = WithClientIP(ctx, clientIP(r))
	ctx = WithUserAgent(ctx, r.UserAgent())
	ctx = WithHeaders(ctx, cloneAllowedHeaders(headers))

	return ctx, token, nil
}

// singleHeader 读取请求头中指定 key 的唯一值，重复出现时返回 RequestInvalidHeader。
func singleHeader(headers http.Header, key string) (string, error) {
	values := headers.Values(key)
	if len(values) > 1 {
		return "", fmt.Errorf("%w: duplicate %s", errors.RequestInvalidHeader, key)
	}
	if len(values) == 0 {
		return "", nil
	}
	return strings.TrimSpace(values[0]), nil
}

// parseAuthorization 解析 Authorization 请求头，仅提取 Bearer 前缀后的 token；
// 非 Bearer 格式的原样保留，空值返回空字符串。
func parseAuthorization(authorization string) (string, error) {
	authorization = strings.TrimSpace(authorization)
	if authorization == "" {
		return "", nil
	}

	parts := strings.Fields(authorization)
	if !strings.EqualFold(parts[0], "Bearer") {
		return authorization, nil
	}
	if len(parts) != 2 || parts[1] == "" {
		return "", errors.InvalidToken
	}
	return parts[1], nil
}

// clientIP 按 X-Forwarded-For → X-Real-Ip → RemoteAddr 的顺序提取客户端 IP。
func clientIP(r *http.Request) string {
	for _, value := range r.Header.Values(headerForwardedFor) {
		for part := range strings.SplitSeq(value, ",") {
			if ip := normalizedIP(part); ip != "" {
				return ip
			}
		}
	}
	if ip := normalizedIP(r.Header.Get(headerRealIP)); ip != "" {
		return ip
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return normalizedIP(host)
	}
	return normalizedIP(r.RemoteAddr)
}

// normalizedIP 规范化 IP 字符串，非法值返回空字符串。
func normalizedIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}
