package interceptor

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StreamServerInterceptor applies exactly the unary method/credential policy
// before invoking a streaming handler, including named secrets with no fallback.
// Authentication occurs at stream admission, not once per response message.
func StreamServerInterceptor(cfg AuthConfig) grpc.StreamServerInterceptor {
	authenticate := UnaryServerInterceptor(cfg)
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		_, err := authenticate(stream.Context(), nil, &grpc.UnaryServerInfo{Server: srv, FullMethod: info.FullMethod},
			func(ctx context.Context, _ interface{}) (interface{}, error) {
				limit := cfg.ServiceMethods[info.FullMethod].MaxStreamDuration
				if userLimit := cfg.StreamMaxDurations[info.FullMethod]; userLimit > 0 && (limit <= 0 || userLimit < limit) {
					limit = userLimit
				}
				if limit > 0 {
					if err := ctx.Err(); err != nil {
						return nil, status.FromContextError(err).Err()
					}
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > limit {
						// Deriving a shorter context would not cancel the underlying
						// transport's RecvMsg/SendMsg. Reject instead, before RecvMsg.
						return nil, status.Error(codes.InvalidArgument, "stream requires a bounded transport deadline")
					}
				}
				return nil, handler(srv, &authenticatedStream{ServerStream: stream, ctx: ctx})
			})
		return err
	}
}

type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context { return s.ctx }
