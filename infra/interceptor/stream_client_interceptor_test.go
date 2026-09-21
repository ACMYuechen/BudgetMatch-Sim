package interceptor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestStreamClientPropagatesTrustedTokenAndTransportContext(t *testing.T) {
	for _, source := range []string{"rpc", "http", "none"} {
		t.Run(source, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			original := metadata.Pairs("authorization", "Bearer untrusted", "x-trace", "kept")
			ctx = metadata.NewOutgoingContext(ctx, original)
			if source == "rpc" {
				ctx = context.WithValue(ctx, "token", "http-token")
				ctx = context.WithValue(ctx, ContextKeyToken, "trusted-token")
			} else if source == "http" {
				ctx = context.WithValue(ctx, "token", "trusted-token")
			}
			handlerErr := errors.New("streamer failed")
			_, err := StreamClientInterceptor()(ctx, &grpc.StreamDesc{ServerStreams: true}, nil, "/agent/test",
				func(actual context.Context, desc *grpc.StreamDesc, _ *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
					require.Equal(t, "/agent/test", method)
					require.True(t, desc.ServerStreams)
					require.Len(t, opts, 1)
					md, _ := metadata.FromOutgoingContext(actual)
					want := "Bearer trusted-token"
					if source == "none" {
						want = "Bearer untrusted" // explicit caller credentials; no invented identity
					}
					require.Equal(t, []string{want}, md.Get("authorization"))
					require.Equal(t, []string{"kept"}, md.Get("x-trace"))
					before, _ := ctx.Deadline()
					after, _ := actual.Deadline()
					require.Equal(t, before, after)
					cancel()
					require.ErrorIs(t, actual.Err(), context.Canceled)
					return nil, handlerErr
				}, grpc.WaitForReady(false))
			require.ErrorIs(t, err, handlerErr)
			require.Equal(t, []string{"Bearer untrusted"}, original.Get("authorization"))
			md, _ := metadata.FromOutgoingContext(ctx)
			require.Equal(t, original, md)
		})
	}
}
