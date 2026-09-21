package recommendservicelogic_test

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/role"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	server "budgetmatch-sim/services/rpc/agent/internal/server/recommendservice"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const streamTestSecret = "agent-stream-only-synthetic-test-secret"

type streamAgent struct {
	calls atomic.Int32
	run   func(context.Context, agentcore.Input) (*agentcore.Result, error)
}

func (*streamAgent) Name() string { return "test.stream" }
func (a *streamAgent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	a.calls.Add(1)
	if a.run != nil {
		return a.run(ctx, input)
	}
	return &agentcore.Result{Summary: "saved snapshot"}, nil
}

type streamFinalizer struct {
	calls atomic.Int32
	run   func(context.Context, *agentcore.Result) error
}

func (f *streamFinalizer) Finalize(ctx context.Context, result *agentcore.Result) error {
	f.calls.Add(1)
	if f.run != nil {
		return f.run(ctx, result)
	}
	return nil
}

type streamStore struct {
	*memory.InMemory
	saves atomic.Int32
	save  func(context.Context, memory.SaveTurnReq) (memory.Conversation, memory.Turn, error)
}

func newStreamStore() *streamStore {
	return &streamStore{InMemory: memory.NewInMemory(memory.Conf{})}
}
func (s *streamStore) SaveTurn(ctx context.Context, req memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
	s.saves.Add(1)
	if s.save != nil {
		return s.save(ctx, req)
	}
	return s.InMemory.SaveTurn(ctx, req)
}

type streamHarness struct {
	client   pb.RecommendServiceClient
	conn     *grpc.ClientConn
	finished chan error
}

func newStreamHarness(t *testing.T, service *recommend.Service) *streamHarness {
	t.Helper()
	h := &streamHarness{finished: make(chan error, 32)}
	listener := bufconn.Listen(1 << 20)
	policy := (config.Config{JwtAuth: auth.Config{Secret: streamTestSecret}}).RPCAuthConfig()
	srv := grpc.NewServer(grpc.UnaryInterceptor(interceptor.UnaryServerInterceptor(policy)),
		grpc.ChainStreamInterceptor(interceptor.StreamServerInterceptor(policy),
			func(srv interface{}, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
				err := handler(srv, stream)
				h.finished <- err
				return err
			}))
	pb.RegisterRecommendServiceServer(srv, server.NewRecommendServiceServer(&svc.ServiceContext{RecommendService: service}))
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = srv.Serve(listener) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = listener.Close()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("in-memory RPC server did not stop")
		}
	})
	conn, err := grpc.NewClient("passthrough:///agent-stream-test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithUnaryInterceptor(interceptor.UnaryClientInterceptor()), grpc.WithStreamInterceptor(interceptor.StreamClientInterceptor()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	h.conn, h.client = conn, pb.NewRecommendServiceClient(conn)
	return h
}

func streamToken(t *testing.T, user string) string {
	t.Helper()
	token, err := auth.GenerateToken(user, streamTestSecret, 3600, role.RoleUser)
	require.NoError(t, err)
	return token
}

func streamClientContext(t *testing.T, user string) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx := context.WithValue(context.Background(), "token", streamToken(t, user))
	return context.WithTimeout(ctx, 5*time.Second)
}

func streamRequest() *pb.RecommendReq {
	return &pb.RecommendReq{Query: "键盘", BudgetCents: 30000, MaxItems: 2, ConversationId: "conversation", TurnId: "turn"}
}

func readStream(t *testing.T, client pb.RecommendServiceClient, ctx context.Context, req *pb.RecommendReq) ([]*pb.RecommendStreamEvent, error) {
	t.Helper()
	stream, err := client.RecommendStream(ctx, req)
	if err != nil {
		return nil, err
	}
	var events []*pb.RecommendStreamEvent
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return events, nil
		}
		if err != nil {
			return events, err
		}
		events = append(events, event)
		if len(events) > streamcontract.MaxProgressEvents+3 {
			t.Fatal("v1 emitted too many frames")
		}
	}
}

func assertStreamEnvelope(t *testing.T, events []*pb.RecommendStreamEvent, names ...string) {
	t.Helper()
	require.Len(t, events, len(names))
	for i, event := range events {
		require.Equal(t, names[i], event.Event)
		require.Equal(t, uint32(streamcontract.Version), event.SchemaVersion)
		require.Equal(t, uint64(i+1), event.Sequence)
		require.NotEmpty(t, event.ExecutionId)
		require.NotEmpty(t, event.ConversationId)
		require.NotEmpty(t, event.TurnId)
		require.Equal(t, events[0].ExecutionId, event.ExecutionId)
		require.Equal(t, events[0].ConversationId, event.ConversationId)
		require.Equal(t, events[0].TurnId, event.TurnId)
		switch event.Event {
		case streamcontract.Accepted:
			require.NotNil(t, event.GetAccepted())
		case streamcontract.Final:
			require.NotNil(t, event.GetFinal())
			require.Equal(t, event.ConversationId, event.GetFinal().ConversationId)
			require.Equal(t, event.TurnId, event.GetFinal().TurnId)
		case streamcontract.Error:
			require.NotNil(t, event.GetError())
		case streamcontract.Done:
			require.NotNil(t, event.GetDone())
		case streamcontract.AnswerDelta:
			require.NotNil(t, event.GetAnswerDelta())
			require.True(t, event.GetAnswerDelta().Provisional)
		case streamcontract.ToolStarted, streamcontract.ToolCompleted:
			require.NotNil(t, event.GetTool())
		}
	}
}

func waitStreamFinished(t *testing.T, h *streamHarness) error {
	t.Helper()
	select {
	case err := <-h.finished:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("stream handler did not exit; execution/lock may be leaked")
		return nil
	}
}

type fakeRecommendStream struct {
	grpc.ServerStream
	ctx    context.Context
	events []*pb.RecommendStreamEvent
	send   func(*pb.RecommendStreamEvent) error
}

func (s *fakeRecommendStream) Context() context.Context { return s.ctx }
func (s *fakeRecommendStream) Send(event *pb.RecommendStreamEvent) error {
	if s.send != nil {
		if err := s.send(event); err != nil {
			return err
		}
	}
	s.events = append(s.events, event)
	return nil
}
