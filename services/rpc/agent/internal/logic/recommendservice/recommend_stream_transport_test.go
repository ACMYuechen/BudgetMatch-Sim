package recommendservicelogic_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/infra/serviceauth"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestRecommendStreamTransportAdmission(t *testing.T) {
	runner, store := &streamAgent{}, newStreamStore()
	h := newStreamHarness(t, recommend.NewService(runner, nil, store))
	expired, err := auth.GenerateToken("user", streamTestSecret, -60, role.RoleUser)
	require.NoError(t, err)
	forged, err := auth.GenerateToken("user", "wrong-signature", 3600, role.RoleUser)
	require.NoError(t, err)
	wrongRole, err := auth.GenerateToken("user", streamTestSecret, 3600, 200)
	require.NoError(t, err)
	service, err := serviceauth.GenerateScopedToken(serviceauth.ServiceAgent, serviceauth.ServiceMall,
		serviceauth.PurposeProductIndexRead, "index-test-secret-independent-32-chars", time.Minute)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, token string
		duration    time.Duration
		code        codes.Code
	}{
		{"missing", "", 5 * time.Second, codes.Unauthenticated},
		{"expired", expired, 5 * time.Second, codes.Unauthenticated},
		{"forged", forged, 5 * time.Second, codes.Unauthenticated},
		{"wrong role", wrongRole, 5 * time.Second, codes.Unauthenticated},
		{"index credential", service, 5 * time.Second, codes.Unauthenticated},
		{"missing deadline", streamToken(t, "user"), 0, codes.InvalidArgument},
		{"long deadline", streamToken(t, "user"), time.Minute, codes.InvalidArgument},
		{"expired deadline", streamToken(t, "user"), -time.Second, codes.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Same-name string context and forged metadata do not establish identity.
			ctx := context.WithValue(context.Background(), "user_id", "victim")
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("user_id", "victim", "role", "1"))
			ctx = context.WithValue(ctx, "token", tc.token)
			if tc.duration != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.duration)
				defer cancel()
			}
			events, err := readStream(t, h.client, ctx, streamRequest())
			require.Equal(t, tc.code, status.Code(err))
			require.Empty(t, events)
		})
	}
	require.Zero(t, runner.calls.Load())
	require.Zero(t, store.saves.Load())
}

func TestRecommendStreamRejectsBeforeReceivingRequest(t *testing.T) {
	runner, store := &streamAgent{}, newStreamStore()
	h := newStreamHarness(t, recommend.NewService(runner, nil, store))
	for _, token := range []string{"", streamToken(t, "user")} {
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "token", token))
		// Intentionally omit SendMsg: authentication/deadline admission must not
		// wait indefinitely for the first request body on an unauthorized stream.
		stream, err := h.conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, pb.RecommendService_RecommendStream_FullMethodName)
		require.NoError(t, err)
		finished := make(chan error, 1)
		go func() { finished <- stream.RecvMsg(&pb.RecommendStreamEvent{}) }()
		select {
		case err := <-finished:
			want := codes.Unauthenticated
			if token != "" {
				want = codes.InvalidArgument
			}
			require.Equal(t, want, status.Code(err))
		case <-time.After(5 * time.Second):
			cancel()
			<-finished
			t.Fatal("admission waited for a request body")
		}
		cancel()
	}
	require.Zero(t, runner.calls.Load())
	require.Zero(t, store.saves.Load())
}

func TestRecommendStreamPersistsThenReplaysAcrossUnary(t *testing.T) {
	runner, store, finalizer := &streamAgent{}, newStreamStore(), &streamFinalizer{}
	h := newStreamHarness(t, recommend.NewService(runner, nil, store).WithFinalizer(finalizer))
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	req := streamRequest()
	req.ConversationId, req.TurnId = "", ""
	events, err := readStream(t, h.client, ctx, req)
	require.NoError(t, err)
	assertStreamEnvelope(t, events, streamcontract.Accepted, streamcontract.Final, streamcontract.Done)
	require.True(t, events[2].GetDone().Ok)
	require.False(t, events[2].GetDone().Replayed)
	require.Zero(t, proto.Size(events[0].GetAccepted()), "admission must not echo the query or tool arguments")
	req.ConversationId, req.TurnId = events[0].ConversationId, events[0].TurnId
	_, found, err := store.FindTurn(ctx, "user", req.ConversationId, req.TurnId)
	require.NoError(t, err)
	require.True(t, found)
	replay, err := readStream(t, h.client, ctx, req)
	require.NoError(t, err)
	assertStreamEnvelope(t, replay, streamcontract.Final, streamcontract.Done)
	require.True(t, replay[1].GetDone().Replayed)
	require.NotEqual(t, events[0].ExecutionId, replay[0].ExecutionId)
	require.True(t, proto.Equal(events[1].GetFinal(), replay[0].GetFinal()))
	unary, err := h.client.Recommend(ctx, req)
	require.NoError(t, err)
	require.True(t, proto.Equal(unary, replay[0].GetFinal()))
	require.Equal(t, int32(1), runner.calls.Load())
	require.Equal(t, int32(1), finalizer.calls.Load())
	require.Equal(t, int32(1), store.saves.Load())
	req.Query = "changed request"
	conflict, err := readStream(t, h.client, ctx, req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.Empty(t, conflict)
	// The inverse direction also replays, without changing the wire response.
	req.TurnId = "unary-first"
	unary, err = h.client.Recommend(ctx, req)
	require.NoError(t, err)
	replay, err = readStream(t, h.client, ctx, req)
	require.NoError(t, err)
	assertStreamEnvelope(t, replay, streamcontract.Final, streamcontract.Done)
	require.True(t, proto.Equal(unary, replay[0].GetFinal()))
	require.Equal(t, int32(2), runner.calls.Load())
	require.Equal(t, int32(2), finalizer.calls.Load())
	require.Equal(t, int32(2), store.saves.Load())
}

func TestRecommendStreamUsesTrustedUserNamespaceAndDownstreamToken(t *testing.T) {
	runner, store := &streamAgent{}, newStreamStore()
	runner.run = func(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
		token := interceptor.TokenFromContext(ctx)
		user, err := auth.GetUserIdFromToken(token, streamTestSecret)
		if err != nil || user != input.UserId {
			return nil, errors.New("identity lost before Agent")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > streamcontract.MaxDuration {
			return nil, errors.New("downstream deadline missing")
		}
		return &agentcore.Result{Summary: "owned by " + input.UserId}, nil
	}
	h := newStreamHarness(t, recommend.NewService(runner, nil, store))
	for _, user := range []string{"owner", "other", "owner"} {
		ctx, cancel := streamClientContext(t, user)
		// The interceptor must replace an existing authorization value, not
		// append the trusted token after attacker-controlled metadata.
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+streamToken(t, "victim"), "user_id", "victim"))
		events, err := readStream(t, h.client, ctx, streamRequest())
		cancel()
		require.NoError(t, err)
		final := events[len(events)-2].GetFinal()
		require.Equal(t, "owned by "+user, final.Summary)
	}
	require.Equal(t, int32(2), runner.calls.Load())
	for _, user := range []string{"owner", "other", "victim"} {
		_, found, err := store.FindTurn(context.Background(), user, "conversation", "turn")
		require.NoError(t, err)
		require.Equal(t, user != "victim", found)
	}
}

func TestRecommendStreamCancelStopsLateResultWithoutFallbackOrSave(t *testing.T) {
	for _, stop := range []string{"cancel", "deadline"} {
		t.Run(stop, func(t *testing.T) {
			started := make(chan struct{})
			runner, fallback, store, finalizer := &streamAgent{}, &streamAgent{}, newStreamStore(), &streamFinalizer{}
			runner.run = func(ctx context.Context, _ agentcore.Input) (*agentcore.Result, error) {
				if runner.calls.Load() == 1 {
					close(started)
					<-ctx.Done()
				}
				return &agentcore.Result{Summary: "late valid-looking result"}, nil
			}
			h := newStreamHarness(t, recommend.NewService(fallback, runner, store).WithFinalizer(finalizer))
			ctx, cancel := streamClientContext(t, "user")
			defer cancel()
			if stop == "deadline" {
				var shortCancel context.CancelFunc
				ctx, shortCancel = context.WithTimeout(ctx, time.Second)
				defer shortCancel()
			}
			stream, err := h.client.RecommendStream(ctx, streamRequest())
			require.NoError(t, err)
			first, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(t, streamcontract.Accepted, first.Event)
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("Agent did not start")
			}
			want := codes.DeadlineExceeded
			if stop == "cancel" {
				cancel()
				want = codes.Canceled
			}
			_, err = stream.Recv()
			require.Equal(t, want, status.Code(err))
			require.Error(t, waitStreamFinished(t, h))
			require.Zero(t, fallback.calls.Load())
			require.Zero(t, finalizer.calls.Load())
			require.Zero(t, store.saves.Load())
			// Completion must release the shared conversation lock for a retry.
			retryCtx, retryCancel := streamClientContext(t, "user")
			defer retryCancel()
			events, err := readStream(t, h.client, retryCtx, streamRequest())
			require.NoError(t, err)
			assertStreamEnvelope(t, events, streamcontract.Accepted, streamcontract.Final, streamcontract.Done)
			require.Equal(t, int32(2), runner.calls.Load())
			require.Equal(t, int32(1), store.saves.Load())
		})
	}
}

func TestRecommendStreamSharesLockWithUnaryAndCancelsWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	runner, store := &streamAgent{}, newStreamStore()
	runner.run = func(ctx context.Context, _ agentcore.Input) (*agentcore.Result, error) {
		close(started)
		select {
		case <-release:
			return &agentcore.Result{Summary: "unary saved"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	h := newStreamHarness(t, recommend.NewService(runner, nil, store))
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	unaryDone := make(chan error, 1)
	go func() { _, err := h.client.Recommend(ctx, streamRequest()); unaryDone <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("unary did not start")
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	events, err := readStream(t, h.client, waitCtx, streamRequest())
	waitCancel()
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.Empty(t, events, "waiting for lock must not announce accepted execution")
	require.Error(t, waitStreamFinished(t, h))
	require.Equal(t, int32(1), runner.calls.Load())
	close(release)
	require.NoError(t, <-unaryDone)
	replay, err := readStream(t, h.client, ctx, streamRequest())
	require.NoError(t, err)
	assertStreamEnvelope(t, replay, streamcontract.Final, streamcontract.Done)
	require.True(t, replay[1].GetDone().Replayed)
	require.Equal(t, int32(1), store.saves.Load())
}

func TestRecommendStreamPreservesDemandGuardAndInvalidInput(t *testing.T) {
	runner, store := &streamAgent{}, newStreamStore()
	h := newStreamHarness(t, recommend.NewService(runner, nil, store))
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	_, err := h.client.PlanDemand(ctx, &pb.PlanDemandReq{Query: "desk", BudgetCents: 30000,
		ConversationId: "conversation", TurnId: "plan", DemandPatch: `{"schema_version":1,"required":{"operation":"replace","values":["keyboard"]}}`})
	require.NoError(t, err)
	events, err := readStream(t, h.client, ctx, streamRequest())
	require.Empty(t, events)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	for _, req := range []*pb.RecommendReq{{}, {Query: "desk", ConversationId: " padded "}, {Query: "desk", BudgetCents: -1}, {Query: "desk", MaxItems: 1000}} {
		events, err := readStream(t, h.client, ctx, req)
		require.Empty(t, events)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	require.Zero(t, runner.calls.Load())
	require.Equal(t, int32(1), store.saves.Load(), "only the planning turn is saved")
	_, found, err := store.FindTurn(ctx, "user", "conversation", "turn")
	require.NoError(t, err)
	require.False(t, found)
}

func TestRecommendStreamAcceptedPrecedesAgentCompletion(t *testing.T) {
	release := make(chan struct{})
	runner := &streamAgent{run: func(ctx context.Context, _ agentcore.Input) (*agentcore.Result, error) {
		select {
		case <-release:
			return &agentcore.Result{Summary: "finished"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	store := newStreamStore()
	h := newStreamHarness(t, recommend.NewService(runner, nil, store))
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	stream, err := h.client.RecommendStream(ctx, streamRequest())
	require.NoError(t, err)
	event, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, event.GetAccepted())
	require.Zero(t, store.saves.Load())
	close(release)
	event, err = stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, event.GetFinal())
	require.Equal(t, int32(1), store.saves.Load())
	event, err = stream.Recv()
	require.NoError(t, err)
	require.True(t, event.GetDone().Ok)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}
