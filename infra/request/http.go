package request

import (
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

// FromHTTPRequest 从 HTTP 请求中解析业务请求信息。
//
// 该函数只负责提取 Token，不校验 Token 签名和 claims。
func FromHTTPRequest(r *http.Request) (*Request, error) {
	if r == nil {
		return nil, errors.RequestNilHTTP
	}

	authorization, err := singleHeader(r.Header, HeaderAuthorization)
	if err != nil {
		return nil, err
	}
	token, err := parseAuthorization(authorization)
	if err != nil {
		return nil, err
	}

	requestID := strings.TrimSpace(r.Header.Get(HeaderRequestID))

	headers := make(map[string]string, len(allowedHeaders))
	for key := range allowedHeaders {
		value := strings.TrimSpace(r.Header.Get(key))
		if value != "" {
			headers[key] = value
		}
	}

	return &Request{
		Token:     token,
		RequestID: requestID,
		ClientIP:  clientIP(r),
		UserAgent: r.UserAgent(),
		Headers:   cloneAllowedHeaders(headers),
	}, nil
}

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

func normalizedIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}
