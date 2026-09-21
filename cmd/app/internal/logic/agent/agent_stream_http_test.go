package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/interceptor"
	middleware "budgetmatch-sim/infra/middleware"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest/handler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type gatewayRPCServer struct {
	pb.UnimplementedRecommendServiceServer
	gate     chan struct{}
	finished chan struct{}
	observed chan context.Context
	calls    atomic.Int32
}

func (s *gatewayRPCServer) RecommendStream(in *pb.RecommendReq, stream pb.RecommendService_RecommendStreamServer) error {
	s.calls.Add(1)
	defer close(s.finished)
	s.observed <- stream.Context()
	for i, frame := range gatewayFrames() {
		if i == 4 {
			select {
			case <-s.gate:
			case <-stream.Context().Done():
				return stream.Context().Err()
			}
		}
		frame.ConversationId, frame.TurnId = in.ConversationId, in.TurnId
		if final := frame.GetFinal(); final != nil {
			final.ConversationId, final.TurnId = in.ConversationId, in.TurnId
		}
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
	return nil
}

type gatewayPBClient struct {
	pb.RecommendServiceClient
}

func TestGatewayHTTPRPCStreamsBeforeCompletionAndCancels(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "cancel"}[canceled], func(t *testing.T) {
			const secret = "synthetic-gateway-stream-secret"
			rpc := &gatewayRPCServer{gate: make(chan struct{}), finished: make(chan struct{}), observed: make(chan context.Context, 1)}
			listener := bufconn.Listen(1 << 20)
			server := grpc.NewServer(grpc.StreamInterceptor(interceptor.StreamServerInterceptor(interceptor.AuthConfig{Secret: secret,
				StreamMaxDurations: map[string]time.Duration{pb.RecommendService_RecommendStream_FullMethodName: streamcontract.MaxDuration}})))
			pb.RegisterRecommendServiceServer(server, rpc)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); _ = listener.Close() })
			conn, err := grpc.NewClient("passthrough:///gateway-stream", grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
				grpc.WithStreamInterceptor(interceptor.StreamClientInterceptor()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			token, err := auth.GenerateToken("u", secret, 3600, role.RoleUser)
			require.NoError(t, err)
			svcCtx := &svc.ServiceContext{AgentClient: gatewayPBClient{RecommendServiceClient: pb.NewRecommendServiceClient(conn)}, Validator: validator.New()}
			// Synthetic HTTP authentication injects the same trusted keys as the
			// application middleware; the RPC validates the actual signed JWT.
			endpoint := middleware.NewLoggingMiddleware("").Handle(func(w http.ResponseWriter, r *http.Request) {
				ctx := context.WithValue(request.WithUserId(r.Context(), "u"), "token", token)
				NewAgentRecommendStreamLogic(ctx, svcCtx).ServeHTTP(w, r.WithContext(ctx))
			})
			httpServer := httptest.NewServer(handler.TimeoutHandler(time.Minute)(handler.LogHandler(endpoint)))
			t.Cleanup(httpServer.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpServer.URL, strings.NewReader(`{"query":"PRIVATE","conversation_id":"c","turn_id":"t","stream_version":1}`))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			response, err := httpServer.Client().Do(req)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Contains(t, response.Header.Get("Content-Type"), "version=1")
			scanner := bufio.NewScanner(response.Body)
			var events []types.AgentStreamEvent
			for scanner.Scan() {
				if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
					var event types.AgentStreamEvent
					require.NoError(t, json.Unmarshal([]byte(data), &event))
					events = append(events, event)
					if event.AnswerDelta != nil {
						break
					}
				}
			}
			require.Len(t, events, 4, "actual flushed delta must arrive before the RPC gate opens")
			observed := <-rpc.observed
			require.Equal(t, "u", observed.Value(interceptor.ContextKeyUserId))
			deadline, ok := observed.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), streamcontract.MaxDuration)
			select {
			case <-rpc.finished:
				t.Fatal("RPC completed before delta observed")
			default:
			}
			if canceled {
				cancel()
			} else {
				close(rpc.gate)
			}
			for scanner.Scan() {
				if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
					var event types.AgentStreamEvent
					require.NoError(t, json.Unmarshal([]byte(data), &event))
					events = append(events, event)
				}
			}
			select {
			case <-rpc.finished:
			case <-time.After(5 * time.Second):
				t.Fatal("upstream RPC leaked after HTTP completion/cancel")
			}
			if canceled {
				require.Len(t, events, 4)
			} else {
				require.NoError(t, scanner.Err())
				require.Len(t, events, 6)
				require.True(t, events[5].Done.Ok)
			}
			require.Equal(t, int32(1), rpc.calls.Load())
		})
	}
}

type deadlineWriter struct {
	header   http.Header
	deadline time.Time
	mode     string
	writes   int
}

func (w *deadlineWriter) Header() http.Header { return w.header }
func (*deadlineWriter) WriteHeader(int)       {}
func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
func (w *deadlineWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.mode == "slow" {
		<-time.After(time.Until(w.deadline))
		return 0, context.DeadlineExceeded
	}
	if w.mode == "short" {
		return len(data) - 1, nil
	}
	return len(data), nil
}
func (*deadlineWriter) Flush() {}
func (w *deadlineWriter) FlushError() error {
	if w.mode == "flush" {
		return errors.New("PRIVATE flush failure")
	}
	return nil
}

func TestGatewayHTTPSlowWriteAndFlushFailureStopRPCReads(t *testing.T) {
	for _, mode := range []string{"slow", "flush", "short"} {
		t.Run(mode, func(t *testing.T) {
			client := &gatewayClient{source: &gatewayStream{frames: gatewayFrames()}}
			svcCtx := &svc.ServiceContext{AgentClient: client, Validator: validator.New()}
			ctx, cancel := context.WithTimeout(request.WithUserId(context.Background(), "u"), 50*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "/stream", strings.NewReader(`{"query":"desk","conversation_id":"c","turn_id":"t","stream_version":1}`)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			writer := &deadlineWriter{header: make(http.Header), mode: mode}
			NewAgentRecommendStreamLogic(ctx, svcCtx).ServeHTTP(writer, req)
			require.Equal(t, 1, writer.writes)
			require.Equal(t, 1, client.source.received)
			require.Error(t, client.ctx.Err())
			require.Zero(t, client.unary)
			deadline, _ := ctx.Deadline()
			require.Equal(t, deadline, writer.deadline, "deadline must cover net/http's final flush after handler return")
		})
	}
}

func TestGatewayHTTPNegotiationValidationAndLegacy(t *testing.T) {
	for _, mode := range []string{"legacy", "missing accept", "invalid version", "large body", "no deadline writer", "invalid field"} {
		t.Run(mode, func(t *testing.T) {
			client := &gatewayClient{}
			svcCtx := &svc.ServiceContext{AgentClient: client, Validator: validator.New()}
			body := `{"query":"desk","conversation_id":"c","turn_id":"t","stream_version":1}`
			if mode == "legacy" {
				body = `{"query":"desk"}`
			}
			if mode == "invalid version" {
				body = `{"query":"desk","stream_version":2}`
			}
			if mode == "large body" {
				body = `{"query":"` + strings.Repeat("x", 17000) + `"}`
			}
			if mode == "invalid field" {
				body = `{"query":"PRIVATE","max_items":99}`
			}
			ctx := request.WithUserId(context.Background(), "u")
			req := httptest.NewRequest(http.MethodPost, "/stream", strings.NewReader(body)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			if mode != "missing accept" {
				req.Header.Set("Accept", "text/event-stream")
			}
			response := httptest.NewRecorder()
			NewAgentRecommendStreamLogic(ctx, svcCtx).ServeHTTP(response, req)
			if mode == "legacy" {
				require.Equal(t, 1, client.unary)
				require.Equal(t, http.StatusOK, response.Code)
				require.Contains(t, response.Body.String(), "event: recommendation.final")
				require.Contains(t, response.Body.String(), "event: done")
				require.NotContains(t, response.Header().Get("Content-Type"), "version=1")
			} else {
				require.GreaterOrEqual(t, response.Code, 400)
				require.Zero(t, client.unary)
				require.NotContains(t, response.Body.String(), "PRIVATE")
			}
			require.Zero(t, client.streams)
		})
	}
}
