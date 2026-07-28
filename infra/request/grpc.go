package request

import (
	"context"
	"fmt"
	"net"
	"strings"

	"budgetmatch-sim/infra/errors"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

const (
	metadataAuthorization = "authorization"
	metadataRequestID     = "x-request-id"
	metadataUserAgent     = "user-agent"
)

// FromGRPCContext 从 gRPC incoming metadata 中解析业务请求信息。
//
// 该函数只负责提取 Token，不校验 Token 签名和 claims。
func FromGRPCContext(ctx context.Context) (*Request, error) {
	req := &Request{}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		req.ClientIP = grpcClientIP(ctx)
		return req, nil
	}

	authorization, err := singleMetadata(md, metadataAuthorization)
	if err != nil {
		return nil, err
	}
	req.Token, err = parseAuthorization(authorization)
	if err != nil {
		return nil, err
	}

	req.RequestID = firstMetadata(md, metadataRequestID)
	req.UserAgent = firstMetadata(md, metadataUserAgent)
	req.ClientIP = grpcClientIP(ctx)
	req.Headers = cloneAllowedHeaders(map[string]string{
		HeaderAuthorization: authorization,
		HeaderRequestID:     req.RequestID,
		HeaderUserAgent:     req.UserAgent,
	})
	return req, nil
}

// NewOutgoingContext 将白名单内的请求信息写入 gRPC outgoing metadata。
//
// 已有 metadata 会被保留，相同白名单字段会被覆盖，避免重复追加。
func NewOutgoingContext(ctx context.Context) context.Context {
	req, ok := stored(ctx)
	if !ok {
		return ctx
	}

	if req.Token == "" && req.RequestID == "" {
		return ctx
	}

	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	if req.Token != "" {
		md.Set(metadataAuthorization, "Bearer "+req.Token)
	}
	if req.RequestID != "" {
		md.Set(metadataRequestID, req.RequestID)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

func singleMetadata(md metadata.MD, key string) (string, error) {
	values := md.Get(key)
	if len(values) > 1 {
		return "", fmt.Errorf("%w: duplicate %s", errors.RequestInvalidHeader, key)
	}
	if len(values) == 0 {
		return "", nil
	}
	return strings.TrimSpace(values[0]), nil
}

func firstMetadata(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func grpcClientIP(ctx context.Context) string {
	info, ok := peer.FromContext(ctx)
	if !ok || info.Addr == nil {
		return ""
	}

	host, _, err := net.SplitHostPort(info.Addr.String())
	if err == nil {
		return normalizedIP(host)
	}
	return normalizedIP(info.Addr.String())
}
