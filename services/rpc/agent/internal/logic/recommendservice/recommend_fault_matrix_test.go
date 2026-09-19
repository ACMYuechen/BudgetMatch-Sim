package recommendservicelogic_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Separate Service objects, local locks, Redis connections and bufconn servers.
// This exercises the production RPC/auth/store path in ONE test process, not
// Redis persistence/failover or a real multi-process deployment.
type faultPair struct {
	redis  *miniredis.Miniredis
	a, b   *streamHarness
	stores [2]*memory.Redis
	waits  [2]chan struct{}
}

type redisWaitHook struct{ waiting chan struct{} }

func (*redisWaitHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (*redisWaitHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *redisWaitHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		key := ""
		if len(cmd.Args()) > 1 {
			key, _ = cmd.Args()[1].(string)
		}
		if value, ok := cmd.(*redis.BoolCmd); ok && err == nil && !value.Val() && strings.HasSuffix(key, ":lock") {
			select {
			case h.waiting <- struct{}{}:
			default:
			}
		}
		return err
	}
}

func newFaultPair(t *testing.T, first, second *streamAgent) *faultPair {
	t.Helper()
	p := &faultPair{redis: miniredis.RunT(t)}
	for i := range p.stores {
		client := redis.NewClient(&redis.Options{Addr: p.redis.Addr(), PoolSize: 1,
			MaxRetries: -1, ContextTimeoutEnabled: true})
		t.Cleanup(func() { _ = client.Close() })
		p.waits[i] = make(chan struct{}, 1)
		client.AddHook(&redisWaitHook{waiting: p.waits[i]})
		p.stores[i] = memory.NewRedis(client, memory.Conf{TTL: time.Hour})
	}
	p.a = newStreamHarness(t, recommend.NewService(first, nil, p.stores[0]))
	p.b = newStreamHarness(t, recommend.NewService(second, nil, p.stores[1]))
	return p
}

func faultGate(t *testing.T) (*streamAgent, <-chan struct{}, func()) {
	t.Helper()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	open := sync.OnceFunc(func() { close(release) })
	t.Cleanup(open)
	runner := &streamAgent{run: func(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return &agentcore.Result{Summary: "first " + input.UserId}, nil
		}
	}}
	return runner, entered, open
}

func awaitFaultSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("fault synchronization point was not reached")
	}
}

type unaryFaultResult struct {
	response *pb.RecommendResp
	err      error
}

func beginFaultUnary(client pb.RecommendServiceClient, ctx context.Context, req *pb.RecommendReq) <-chan unaryFaultResult {
	done := make(chan unaryFaultResult, 1)
	go func() {
		response, err := client.Recommend(ctx, req)
		done <- unaryFaultResult{response, err}
	}()
	return done
}

func awaitFaultUnary(t *testing.T, ch <-chan unaryFaultResult) unaryFaultResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("unary request did not finish")
		return unaryFaultResult{}
	}
}

func beginFaultStream(t *testing.T, h *streamHarness, ctx context.Context) (pb.RecommendService_RecommendStreamClient, *pb.RecommendStreamEvent) {
	t.Helper()
	stream, err := h.client.RecommendStream(ctx, streamRequest())
	require.NoError(t, err)
	accepted, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, streamcontract.Accepted, accepted.Event)
	return stream, accepted
}

func finishFaultStream(t *testing.T, stream pb.RecommendService_RecommendStreamClient, accepted *pb.RecommendStreamEvent, successful bool) *pb.RecommendResp {
	t.Helper()
	result, err := stream.Recv()
	require.NoError(t, err)
	done, err := stream.Recv()
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	kind := streamcontract.Error
	if successful {
		kind = streamcontract.Final
	}
	assertStreamEnvelope(t, []*pb.RecommendStreamEvent{accepted, result, done}, streamcontract.Accepted, kind, streamcontract.Done)
	require.Equal(t, successful, done.GetDone().Ok)
	return result.GetFinal()
}

func TestFaultMatrixSharedRedisSerializesAndReplaysAcrossRPCModes(t *testing.T) {
	for _, scenario := range []string{"duplicate", "next turn", "canceled waiter"} {
		t.Run(scenario, func(t *testing.T) {
			first, entered, release := faultGate(t)
			second := &streamAgent{}
			p := newFaultPair(t, first, second)
			ctx, cancel := streamClientContext(t, "user")
			defer cancel()
			stream, accepted := beginFaultStream(t, p.a, ctx)
			awaitFaultSignal(t, entered)
			req := streamRequest()
			if scenario == "next turn" {
				req.TurnId, req.Query, req.BudgetCents, req.MaxItems = "next", "继续", 0, 0
			}
			waitCtx, stopWait := context.WithCancel(ctx)
			defer stopWait()
			pending := beginFaultUnary(p.b.client, waitCtx, req)
			awaitFaultSignal(t, p.waits[1])
			require.Zero(t, second.calls.Load(), "second Service bypassed the storage lock")
			if scenario == "canceled waiter" {
				stopWait()
				require.Equal(t, codes.Canceled, status.Code(awaitFaultUnary(t, pending).err))
			}
			release()
			original := finishFaultStream(t, stream, accepted, true)
			require.NoError(t, waitStreamFinished(t, p.a))
			if scenario == "canceled waiter" {
				pending = beginFaultUnary(p.b.client, ctx, req)
			}
			got := awaitFaultUnary(t, pending)
			require.NoError(t, got.err)
			wantTurns := int64(1)
			if scenario == "next turn" {
				wantTurns = 2
				require.EqualValues(t, 1, second.calls.Load())
				require.EqualValues(t, streamRequest().BudgetCents, got.response.Intent.BudgetCents)
				require.EqualValues(t, streamRequest().MaxItems, got.response.Intent.MaxItems)
			} else {
				require.True(t, proto.Equal(original, got.response), "unary rollback must replay the saved stream result")
				require.Zero(t, second.calls.Load())
			}
			conversation, turns, total, exists, err := p.stores[1].ListTurns(ctx, "user", "conversation", 1, 20)
			require.NoError(t, err)
			require.True(t, exists)
			require.Equal(t, wantTurns, total)
			require.Equal(t, wantTurns, conversation.Version)
			for i, turn := range turns {
				require.EqualValues(t, i+1, turn.Sequence)
			}
			// Replay through the other Service, without the first Service's
			// local lock or memory. The storage schema stays compatible.
			replay, err := readStream(t, p.b.client, ctx, streamRequest())
			require.NoError(t, err)
			assertStreamEnvelope(t, replay, streamcontract.Final, streamcontract.Done)
			require.True(t, replay[1].GetDone().Replayed)
			require.True(t, proto.Equal(original, replay[0].GetFinal()))
			require.EqualValues(t, 1, first.calls.Load())
		})
	}
}

func TestFaultMatrixSharedRedisHasNoGlobalOrCrossUserLock(t *testing.T) {
	for _, scope := range []string{"different user", "different conversation"} {
		t.Run(scope, func(t *testing.T) {
			first, entered, release := faultGate(t)
			second := &streamAgent{run: func(_ context.Context, in agentcore.Input) (*agentcore.Result, error) {
				return &agentcore.Result{Summary: "second " + in.UserId}, nil
			}}
			p := newFaultPair(t, first, second)
			ctx, cancel := streamClientContext(t, "user")
			defer cancel()
			stream, accepted := beginFaultStream(t, p.a, ctx)
			awaitFaultSignal(t, entered)
			user, req := "user", streamRequest()
			if scope == "different user" {
				user = "other"
			} else {
				req.ConversationId = "other"
			}
			otherCtx, stopOther := streamClientContext(t, user)
			defer stopOther()
			response, err := p.b.client.Recommend(otherCtx, req)
			require.NoError(t, err, "unrelated namespace must finish while first request is still blocked")
			require.Equal(t, "second "+user, response.Summary)
			_, found, err := p.stores[0].FindTurn(ctx, "user", "conversation", "turn")
			require.NoError(t, err)
			require.False(t, found)
			release()
			original := finishFaultStream(t, stream, accepted, true)
			require.Equal(t, "first user", original.Summary)
			require.NoError(t, waitStreamFinished(t, p.a))
			require.EqualValues(t, 1, first.calls.Load())
			require.EqualValues(t, 1, second.calls.Load())
		})
	}
}

func TestFaultMatrixDeletionUsesSharedLockAndAllowsLaterRecreation(t *testing.T) {
	first, entered, release := faultGate(t)
	second := &streamAgent{}
	p := newFaultPair(t, first, second)
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	stream, accepted := beginFaultStream(t, p.a, ctx)
	awaitFaultSignal(t, entered)
	type deleteResult struct {
		response *pb.DeleteConversationResp
		err      error
	}
	done := make(chan deleteResult, 1)
	go func() {
		response, err := p.b.client.DeleteConversation(ctx, &pb.DeleteConversationReq{ConversationId: "conversation"})
		done <- deleteResult{response, err}
	}()
	awaitFaultSignal(t, p.waits[1])
	release()
	finishFaultStream(t, stream, accepted, true)
	require.NoError(t, waitStreamFinished(t, p.a))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.True(t, result.response.Deleted, "delete ran before the in-flight turn committed")
	case <-ctx.Done():
		t.Fatal("delete did not release its lock")
	}
	_, found, err := p.stores[0].FindTurn(ctx, "user", "conversation", "turn")
	require.NoError(t, err)
	require.False(t, found)
	_, err = p.b.client.Recommend(ctx, streamRequest())
	require.NoError(t, err)
	require.EqualValues(t, 1, second.calls.Load(), "deleted data is not replayed; a later request can recreate")
	conversation, _, err := p.stores[0].GetConversation(ctx, "user", "conversation")
	require.NoError(t, err)
	require.EqualValues(t, 1, conversation.TurnCount)
}

func TestFaultMatrixExpiredServiceCannotOverwriteSuccessor(t *testing.T) {
	first, entered, release := faultGate(t)
	second := &streamAgent{}
	p := newFaultPair(t, first, second)
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	stream, accepted := beginFaultStream(t, p.a, ctx)
	awaitFaultSignal(t, entered)
	p.redis.FastForward(2 * time.Minute)
	successor, err := p.b.client.Recommend(ctx, streamRequest())
	require.NoError(t, err)
	release()
	finishFaultStream(t, stream, accepted, false)
	require.NoError(t, waitStreamFinished(t, p.a)) // explicit error + done(false), normal gRPC EOF
	replay, err := readStream(t, p.a.client, ctx, streamRequest())
	require.NoError(t, err)
	assertStreamEnvelope(t, replay, streamcontract.Final, streamcontract.Done)
	require.True(t, replay[1].GetDone().Replayed)
	require.True(t, proto.Equal(successor, replay[0].GetFinal()))
	require.EqualValues(t, 1, first.calls.Load())
	require.EqualValues(t, 1, second.calls.Load())
	conversation, turns, total, exists, err := p.stores[0].ListTurns(ctx, "user", "conversation", 1, 20)
	require.NoError(t, err)
	require.True(t, exists)
	require.EqualValues(t, 1, total)
	require.EqualValues(t, 1, conversation.Version)
	require.Equal(t, successor.Summary, turns[0].Summary)
}
