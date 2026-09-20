package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Real loopback HTTP/proxy/TCP gRPC transport, with a scripted RPC producer.
// No model, business Service/store, existing listener or deployment is used.
type proxyCloseoutRPC struct {
	pb.UnimplementedRecommendServiceServer
	mode                        string
	release, prepared, finished chan struct{}
	observed                    chan context.Context
	stopped                     chan error
	streams, unary              atomic.Int32
}

func (s *proxyCloseoutRPC) Recommend(context.Context, *pb.RecommendReq) (*pb.RecommendResp, error) {
	s.unary.Add(1)
	return nil, status.Error(codes.Unimplemented, "synthetic unary must not run")
}

func (s *proxyCloseoutRPC) RecommendStream(in *pb.RecommendReq, stream pb.RecommendService_RecommendStreamServer) error {
	if s.streams.Add(1) != 1 {
		return status.Error(codes.Aborted, "unexpected second execution")
	}
	defer close(s.finished)
	defer func() { s.stopped <- stream.Context().Err() }()
	s.observed <- stream.Context()
	frames := gatewayFrames()
	switch s.mode {
	case "cancel":
		frames = frames[:4]
	case "missing done":
		frames = frames[:5]
	case "after done":
		frames = append(frames, &pb.RecommendStreamEvent{Event: "future.progress"})
	case "paused proxy":
		frames = frames[:1]
		for range 120 {
			frames = append(frames, &pb.RecommendStreamEvent{Event: "future." + strings.Repeat("p", 57)})
		}
	}
	stampGatewayFrames(frames)
	progressBytes := 0
	for _, frame := range frames {
		frame.ConversationId, frame.TurnId = in.ConversationId, in.TurnId
		if s.mode == "paused proxy" {
			frame.ExecutionId = strings.Repeat("e", 128)
			progressBytes += proto.Size(frame)
			if progressBytes > streamcontract.MaxProgressBytes {
				return status.Error(codes.Internal, "synthetic progress exceeded contract")
			}
		}
		if final := frame.GetFinal(); final != nil {
			final.ConversationId, final.TurnId = in.ConversationId, in.TurnId
		}
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
	close(s.prepared)
	if s.mode == "missing done" || s.mode == "after done" {
		return nil
	}
	select {
	case <-s.release:
		if s.mode == "late error" {
			return status.Error(codes.Unavailable, "PRIVATE synthetic upstream failure")
		}
		return nil
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
}

type proxyCloseoutHarness struct {
	rpc                    *proxyCloseoutRPC
	client                 *http.Client
	url                    string
	gatewayDone, proxyDone chan struct{}
	upstream               chan net.Conn
	paused                 chan struct{}
	writeTimeout           chan struct{}
}

func newProxyCloseoutHarness(t *testing.T, mode string) *proxyCloseoutHarness {
	t.Helper()
	const secret = "synthetic-proxy-closeout-secret"
	h := &proxyCloseoutHarness{
		rpc: &proxyCloseoutRPC{mode: mode, release: make(chan struct{}), prepared: make(chan struct{}),
			finished: make(chan struct{}), observed: make(chan context.Context, 1), stopped: make(chan error, 1)},
		gatewayDone: make(chan struct{}), proxyDone: make(chan struct{}), upstream: make(chan net.Conn, 2),
		paused: make(chan struct{}), writeTimeout: make(chan struct{}, 1),
	}
	rpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	rpcServer := grpc.NewServer(grpc.StreamInterceptor(interceptor.StreamServerInterceptor(interceptor.AuthConfig{Secret: secret,
		StreamMaxDurations: map[string]time.Duration{pb.RecommendService_RecommendStream_FullMethodName: streamcontract.MaxDuration}})))
	pb.RegisterRecommendServiceServer(rpcServer, h.rpc)
	rpcStopped := make(chan struct{})
	go func() { defer close(rpcStopped); _ = rpcServer.Serve(rpcListener) }()
	t.Cleanup(func() {
		rpcServer.Stop()
		_ = rpcListener.Close()
		awaitProxyCloseout(t, rpcStopped, "RPC listener stop")
	})
	conn, err := grpc.NewClient("passthrough:///"+rpcListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStreamInterceptor(interceptor.StreamClientInterceptor()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	token, err := auth.GenerateToken("proxy-user", secret, 3600, role.RoleUser)
	require.NoError(t, err)
	svcCtx := &svc.ServiceContext{AgentClient: gatewayPBClient{RecommendServiceClient: pb.NewRecommendServiceClient(conn)}, Validator: validator.New()}
	endpoint := middleware.NewLoggingMiddleware("").Handle(func(w http.ResponseWriter, r *http.Request) {
		defer close(h.gatewayDone)
		// HTTP identity injection is synthetic; the TCP RPC interceptor checks
		// the actual signed JWT and deadline. This is not HTTP auth acceptance.
		ctx := context.WithValue(request.WithUserId(r.Context(), "proxy-user"), "token", token)
		budget := 5 * time.Second
		if mode == "paused proxy" {
			budget = time.Second
		}
		ctx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()
		NewAgentRecommendStreamLogic(ctx, svcCtx).ServeHTTP(w, r.WithContext(ctx))
	})
	gateway := httptest.NewUnstartedServer(handler.TimeoutHandler(time.Minute)(handler.LogHandler(endpoint)))
	if mode == "paused proxy" {
		gateway.Listener = &proxyWriteProbeListener{Listener: gateway.Listener, timedOut: h.writeTimeout}
	}
	gateway.Start()
	t.Cleanup(func() { gateway.CloseClientConnections(); gateway.Close() })
	target, err := url.Parse(gateway.URL)
	require.NoError(t, err)
	transport := &http.Transport{DisableKeepAlives: true, DisableCompression: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != target.Host {
				return nil, errors.New("test refuses any other upstream target")
			}
			conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, address)
			if err == nil {
				if mode == "paused proxy" {
					if err := conn.(*net.TCPConn).SetReadBuffer(1024); err != nil {
						_ = conn.Close()
						return nil, err
					}
				}
				select {
				case h.upstream <- conn:
				default:
					_ = conn.Close()
					return nil, errors.New("unexpected extra upstream connection")
				}
			}
			return conn, err
		}}
	t.Cleanup(transport.CloseIdleConnections)
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.Transport, reverse.ErrorLog = transport, log.New(io.Discard, "", 0)
	if mode == "paused proxy" {
		reverse.ModifyResponse = func(response *http.Response) error {
			response.Body = &pausedProxyBody{ReadCloser: response.Body, ctx: response.Request.Context(), entered: h.paused}
			return nil
		}
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(h.proxyDone)
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { proxy.CloseClientConnections(); proxy.Close() })
	h.client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DisableCompression: true}}
	t.Cleanup(h.client.CloseIdleConnections)
	h.url = proxy.URL
	return h
}

func (h *proxyCloseoutHarness) request(t *testing.T) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	t.Cleanup(cancel)
	id := "c"
	if h.rpc.mode == "paused proxy" {
		id = strings.Repeat("c", 128)
	}
	body, err := json.Marshal(types.AgentRecommendStreamReq{AgentRecommendReq: types.AgentRecommendReq{
		Query: "PRIVATE synthetic query", ConversationId: id, TurnId: id}, StreamVersion: 1})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	response, err := h.client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, response.Header.Get("Content-Type"), "version=1")
	require.Equal(t, "no-cache, no-transform", response.Header.Get("Cache-Control"))
	require.Equal(t, "no", response.Header.Get("X-Accel-Buffering"))
	select {
	case observed := <-h.rpc.observed:
		require.Equal(t, "proxy-user", observed.Value(interceptor.ContextKeyUserId))
		deadline, ok := observed.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), streamcontract.MaxDuration)
	case <-ctx.Done():
		t.Fatal("RPC did not observe request")
	}
	return response, cancel
}

type proxyReadResult struct {
	event *types.AgentStreamEvent
	err   error
	eof   bool
}

func readProxyCloseout(t *testing.T, response *http.Response) <-chan proxyReadResult {
	t.Helper()
	results := make(chan proxyReadResult, streamcontract.MaxProgressEvents+8)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer close(results)
		scanner := bufio.NewScanner(io.LimitReader(response.Body, streamcontract.MaxHTTPBytes+1))
		scanner.Buffer(make([]byte, 4096), streamcontract.MaxEventBytes)
		var id, kind string
		for scanner.Scan() {
			line := scanner.Text()
			if value, ok := strings.CutPrefix(line, "id: "); ok {
				id = value
			} else if value, ok := strings.CutPrefix(line, "event: "); ok {
				kind = value
			} else if value, ok := strings.CutPrefix(line, "data: "); ok {
				var event types.AgentStreamEvent
				if err := json.Unmarshal([]byte(value), &event); err != nil {
					results <- proxyReadResult{eof: true, err: err}
					return
				}
				if kind != event.Event || id != event.ExecutionId+":"+strconv.FormatUint(event.Sequence, 10) {
					results <- proxyReadResult{eof: true, err: errors.New("SSE framing mismatch")}
					return
				}
				results <- proxyReadResult{event: &event}
			}
		}
		results <- proxyReadResult{eof: true, err: scanner.Err()}
	}()
	t.Cleanup(func() { _ = response.Body.Close(); awaitProxyCloseout(t, finished, "SSE reader stop") })
	return results
}

func nextProxyCloseout(t *testing.T, results <-chan proxyReadResult) proxyReadResult {
	t.Helper()
	select {
	case result, ok := <-results:
		require.True(t, ok, "reader ended without terminal result")
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("proxy stream made no bounded progress")
		return proxyReadResult{}
	}
}

func awaitProxyCloseout(t *testing.T, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not finish", label)
	}
}

func (h *proxyCloseoutHarness) assertStopped(t *testing.T) {
	t.Helper()
	awaitProxyCloseout(t, h.rpc.finished, "upstream RPC")
	awaitProxyCloseout(t, h.gatewayDone, "gateway handler")
	awaitProxyCloseout(t, h.proxyDone, "proxy handler")
	require.Equal(t, int32(1), h.rpc.streams.Load())
	require.Zero(t, h.rpc.unary.Load(), "failed stream must not execute a unary fallback")
}

func TestProxyCloseoutTerminalBarrierOverTCP(t *testing.T) {
	for _, mode := range []string{"complete", "late error", "missing done", "after done"} {
		t.Run(mode, func(t *testing.T) {
			h := newProxyCloseoutHarness(t, mode)
			response, _ := h.request(t)
			results := readProxyCloseout(t, response)
			var events []*types.AgentStreamEvent
			for range 4 {
				result := nextProxyCloseout(t, results)
				require.False(t, result.eof)
				require.NotNil(t, result.event)
				events = append(events, result.event)
			}
			require.NotNil(t, events[3].AnswerDelta, "proxy must flush progress before the RPC ends")
			if mode == "complete" || mode == "late error" {
				awaitProxyCloseout(t, h.rpc.prepared, "RPC terminal sends")
				select {
				case <-results:
					t.Fatal("terminal result escaped before normal RPC EOF")
				case <-time.After(100 * time.Millisecond):
				}
				close(h.rpc.release)
			}
			for {
				result := nextProxyCloseout(t, results)
				if result.eof {
					require.NoError(t, result.err)
					break
				}
				events = append(events, result.event)
			}
			require.Len(t, events, 6)
			for i, event := range events {
				require.EqualValues(t, streamcontract.Version, event.SchemaVersion)
				require.Equal(t, "execution", event.ExecutionId)
				require.Equal(t, uint64(i+1), event.Sequence)
				require.Equal(t, "c", event.ConversationId)
				require.Equal(t, "c", event.TurnId)
				data, err := json.Marshal(event)
				require.NoError(t, err)
				require.NotContains(t, string(data), "PRIVATE")
			}
			require.NotNil(t, events[5].Done)
			if mode == "complete" {
				require.NotNil(t, events[4].Final)
				require.True(t, events[5].Done.Ok)
			} else {
				require.NotNil(t, events[4].Error)
				require.False(t, events[5].Done.Ok)
				for _, event := range events {
					require.Nil(t, event.Final)
				}
			}
			h.assertStopped(t)
		})
	}
}

func TestProxyCloseoutDisconnectCancelsRPCWithoutRetry(t *testing.T) {
	for _, mode := range []string{"client cancel", "client body close", "upstream disconnect"} {
		t.Run(mode, func(t *testing.T) {
			h := newProxyCloseoutHarness(t, "cancel")
			response, cancel := h.request(t)
			results := readProxyCloseout(t, response)
			for range 4 {
				result := nextProxyCloseout(t, results)
				require.False(t, result.eof)
				require.NotNil(t, result.event)
				require.Nil(t, result.event.Final)
			}
			switch mode {
			case "client cancel":
				cancel()
			case "client body close":
				require.NoError(t, response.Body.Close())
			case "upstream disconnect":
				select {
				case conn := <-h.upstream:
					require.NoError(t, conn.Close())
				case <-time.After(time.Second):
					t.Fatal("missing owned upstream connection")
				}
			}
			result := nextProxyCloseout(t, results)
			require.True(t, result.eof)
			require.Error(t, result.err, "aborted HTTP body is not a clean successful EOF")
			h.assertStopped(t)
			require.ErrorIs(t, <-h.rpc.stopped, context.Canceled)
		})
	}
}

// Pause the proxy's upstream body reader. Small buffers on both ends make the
// gateway's actual socket write block until its deadline expires.
type pausedProxyBody struct {
	io.ReadCloser
	ctx     context.Context
	entered chan struct{}
	once    sync.Once
}

func (b *pausedProxyBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

type proxyWriteProbeListener struct {
	net.Listener
	timedOut chan struct{}
}

func (l *proxyWriteProbeListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if err := conn.(*net.TCPConn).SetWriteBuffer(1024); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &proxyWriteProbeConn{Conn: conn, timedOut: l.timedOut}, nil
}

type proxyWriteProbeConn struct {
	net.Conn
	timedOut chan struct{}
}

func (c *proxyWriteProbeConn) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		select {
		case c.timedOut <- struct{}{}:
		default:
		}
	}
	return n, err
}

func TestProxyCloseoutPausedReaderHitsSocketDeadline(t *testing.T) {
	h := newProxyCloseoutHarness(t, "paused proxy")
	response, cancel := h.request(t)
	awaitProxyCloseout(t, h.paused, "paused proxy body reader")
	awaitProxyCloseout(t, h.writeTimeout, "actual gateway socket write timeout")
	awaitProxyCloseout(t, h.gatewayDone, "gateway after write timeout")
	awaitProxyCloseout(t, h.rpc.finished, "RPC after write timeout")
	require.Error(t, <-h.rpc.stopped)
	cancel()
	require.NoError(t, response.Body.Close())
	h.assertStopped(t)
}
