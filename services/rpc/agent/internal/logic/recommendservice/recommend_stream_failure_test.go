package recommendservicelogic_test

import (
	"context"
	"errors"
	"testing"
	"time"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	logic "budgetmatch-sim/services/rpc/agent/internal/logic/recommendservice"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestRecommendStreamPublicFailureNeverEmitsFinal(t *testing.T) {
	for _, stage := range []string{"agent", "unsafe result", "finalizer", "save", "permission", "invalid text"} {
		t.Run(stage, func(t *testing.T) {
			runner, fallback, store, finalizer := &streamAgent{}, &streamAgent{}, newStreamStore(), &streamFinalizer{}
			private := errors.New("PRIVATE password tool arguments")
			wantCode, retryable := int64(apperrors.ECInternal), false
			if stage == "agent" {
				runner.run = func(context.Context, agentcore.Input) (*agentcore.Result, error) { return nil, private }
			} else if stage == "unsafe result" {
				runner.run = func(context.Context, agentcore.Input) (*agentcore.Result, error) {
					return &agentcore.Result{Items: []agentcore.BundleItem{{Id: "bad", PriceCents: 900000, Stock: 1}}, TotalPriceCents: 900000}, nil
				}
			} else if stage == "finalizer" {
				upstream, err := status.New(codes.Unavailable, "PRIVATE upstream text").WithDetails(&errdetails.DebugInfo{Detail: "PRIVATE tool body"})
				require.NoError(t, err)
				finalizer.run = func(context.Context, *agentcore.Result) error { return upstream.Err() }
				retryable = true
			} else if stage == "save" {
				store.save = func(context.Context, memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
					return memory.Conversation{}, memory.Turn{}, private
				}
			} else if stage == "permission" {
				runner.run = func(context.Context, agentcore.Input) (*agentcore.Result, error) {
					return nil, status.Error(codes.PermissionDenied, "PRIVATE forbidden tool")
				}
				wantCode = apperrors.ECUnauthorized
			} else {
				runner.run = func(context.Context, agentcore.Input) (*agentcore.Result, error) {
					return nil, errors.Join(agentcore.ErrBudgetText, private)
				}
				wantCode = apperrors.ECAgentBudgetText
			}
			service := recommend.NewService(runner, nil, store).WithFinalizer(finalizer)
			if stage == "permission" || stage == "invalid text" {
				service = recommend.NewService(fallback, runner, store).WithFinalizer(finalizer)
			}
			h := newStreamHarness(t, service)
			ctx, cancel := streamClientContext(t, "user")
			defer cancel()
			events, err := readStream(t, h.client, ctx, streamRequest())
			require.NoError(t, err, "error+done(false) is a completed event transport, not a successful recommendation")
			assertStreamEnvelope(t, events, streamcontract.Accepted, streamcontract.Error, streamcontract.Done)
			require.Equal(t, wantCode, events[1].GetError().Code)
			require.Equal(t, retryable, events[1].GetError().Retryable)
			require.NotEmpty(t, events[1].GetError().Message)
			require.False(t, events[2].GetDone().Ok)
			require.False(t, events[2].GetDone().Replayed)
			for _, event := range events {
				require.NotContains(t, protojson.Format(event), "PRIVATE")
				require.Nil(t, event.GetFinal())
			}
			require.Zero(t, fallback.calls.Load(), "terminal errors cannot retry via fallback")
			_, found, err := store.FindTurn(ctx, "user", "conversation", "turn")
			require.NoError(t, err)
			require.False(t, found)
		})
	}
}

func TestRecommendStreamSendFailureAbortsOrAllowsCommittedReplay(t *testing.T) {
	for _, failAt := range []string{streamcontract.Accepted, streamcontract.Final, streamcontract.Done} {
		t.Run(failAt, func(t *testing.T) {
			runner, store, finalizer := &streamAgent{}, newStreamStore(), &streamFinalizer{}
			service := recommend.NewService(runner, nil, store).WithFinalizer(finalizer)
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user"), time.Second)
			defer cancel()
			fake := &fakeRecommendStream{ctx: ctx}
			attempts := 0
			fake.send = func(event *pb.RecommendStreamEvent) error {
				attempts++
				_, saved, err := store.FindTurn(ctx, "user", "conversation", "turn")
				require.NoError(t, err)
				require.Equal(t, event.Event != streamcontract.Accepted, saved, "no final before atomic save")
				if event.Event == failAt {
					return status.Error(codes.Unavailable, "PRIVATE broken transport")
				}
				return nil
			}
			err := logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service}).RecommendStream(streamRequest(), fake)
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.NotContains(t, status.Convert(err).Message(), "PRIVATE")
			require.Equal(t, len(fake.events)+1, attempts, "no Send retry or error event after failed Send")
			if failAt == streamcontract.Accepted {
				require.Zero(t, runner.calls.Load())
				require.Zero(t, finalizer.calls.Load())
				require.Zero(t, store.saves.Load())
			} else {
				require.Equal(t, int32(1), store.saves.Load())
			}
			// A fresh call with the same IDs must execute exactly once in total,
			// whether the previous disconnect happened before or after commit.
			retry := &fakeRecommendStream{ctx: ctx}
			require.NoError(t, logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service}).RecommendStream(streamRequest(), retry))
			if failAt == streamcontract.Accepted {
				assertStreamEnvelope(t, retry.events, streamcontract.Accepted, streamcontract.Final, streamcontract.Done)
			} else {
				assertStreamEnvelope(t, retry.events, streamcontract.Final, streamcontract.Done)
				require.True(t, retry.events[1].GetDone().Replayed)
			}
			require.Equal(t, int32(1), runner.calls.Load())
			require.Equal(t, int32(1), finalizer.calls.Load())
			require.Equal(t, int32(1), store.saves.Load())
		})
	}
}

func TestRecommendStreamSlowFakeSendHasBoundedLifetime(t *testing.T) {
	for _, slowAt := range []string{streamcontract.Accepted, streamcontract.Final} {
		t.Run(slowAt, func(t *testing.T) {
			runner, store := &streamAgent{}, newStreamStore()
			service := recommend.NewService(runner, nil, store)
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user"), 50*time.Millisecond)
			defer cancel()
			fake := &fakeRecommendStream{ctx: ctx, send: func(event *pb.RecommendStreamEvent) error {
				if event.Event == slowAt {
					<-ctx.Done() // models transport Send unblocking at its deadline
					return ctx.Err()
				}
				return nil
			}}
			err := logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service}).RecommendStream(streamRequest(), fake)
			require.Equal(t, codes.DeadlineExceeded, status.Code(err))
			wantSaves := int32(0)
			if slowAt == streamcontract.Final {
				wantSaves = 1 // commit cannot be rolled back by failed delivery
			}
			require.Equal(t, wantSaves, store.saves.Load())
			retryCtx, retryCancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user"), time.Second)
			defer retryCancel()
			retry := &fakeRecommendStream{ctx: retryCtx}
			require.NoError(t, logic.NewRecommendStreamLogic(retryCtx, &svc.ServiceContext{RecommendService: service}).RecommendStream(streamRequest(), retry))
			require.Equal(t, int32(1), store.saves.Load(), "stalled Send must not leak a conversation lock")
		})
	}
}

func TestRecommendStreamCancellationDuringFinalizationOrCommit(t *testing.T) {
	for _, stage := range []string{"finalizer", "commit"} {
		t.Run(stage, func(t *testing.T) {
			runner, store, finalizer := &streamAgent{}, newStreamStore(), &streamFinalizer{}
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user"), time.Second)
			defer cancel()
			if stage == "finalizer" {
				finalizer.run = func(context.Context, *agentcore.Result) error { cancel(); return nil }
			} else {
				store.save = func(ctx context.Context, req memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
					conversation, turn, err := store.InMemory.SaveTurn(ctx, req)
					cancel() // durable commit wins the race with cancellation
					return conversation, turn, err
				}
			}
			service := recommend.NewService(runner, nil, store).WithFinalizer(finalizer)
			fake := &fakeRecommendStream{ctx: ctx}
			err := logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service}).RecommendStream(streamRequest(), fake)
			require.Equal(t, codes.Canceled, status.Code(err))
			assertStreamEnvelope(t, fake.events, streamcontract.Accepted)
			_, saved, err := store.FindTurn(context.Background(), "user", "conversation", "turn")
			require.NoError(t, err)
			require.Equal(t, stage == "commit", saved)
			if saved {
				retryCtx, retryCancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user"), time.Second)
				defer retryCancel()
				retry := &fakeRecommendStream{ctx: retryCtx}
				require.NoError(t, logic.NewRecommendStreamLogic(retryCtx, &svc.ServiceContext{RecommendService: service}).RecommendStream(streamRequest(), retry))
				assertStreamEnvelope(t, retry.events, streamcontract.Final, streamcontract.Done)
				require.Equal(t, int32(1), runner.calls.Load())
				require.Equal(t, int32(1), finalizer.calls.Load())
				require.Equal(t, int32(1), store.saves.Load())
			}
		})
	}
}

func TestRecommendStreamRequiresTransportIdentityAndAtomicStore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "trusted"), time.Second)
	defer cancel()
	runner := &streamAgent{}
	service := recommend.NewService(runner, nil, newStreamStore())
	logic := logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service})
	for _, source := range []context.Context{context.Background(), context.WithValue(context.Background(), "user_id", "forged")} {
		// A trusted constructor context must not authorize a different stream.
		fake := &fakeRecommendStream{ctx: source}
		require.ErrorIs(t, logic.RecommendStream(streamRequest(), fake), apperrors.Unauthorized)
		require.Empty(t, fake.events)
	}
	require.ErrorIs(t, logic.RecommendStream(nil, &fakeRecommendStream{ctx: ctx}), apperrors.Invalid)
	service = recommend.NewService(runner, nil, nil)
	_, err := service.RecommendStream(ctx, agentcore.Input{Query: "desk"}, func(context.Context, string, string) error { t.Fatal("accepted without store"); return nil })
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Zero(t, runner.calls.Load())
}

func TestRecommendStreamFinalAndReplaySanitizeToolMetadata(t *testing.T) {
	runner := &streamAgent{run: func(context.Context, agentcore.Input) (*agentcore.Result, error) {
		return &agentcore.Result{ToolsUsed: []agentcore.ToolCall{{Name: "tool.read_file", Success: true, Detail: "PRIVATE file body"},
			{Name: "tool.PRIVATE", Detail: "PRIVATE arguments"}}}, nil
	}}
	h := newStreamHarness(t, recommend.NewService(runner, nil, newStreamStore()))
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	var first *pb.RecommendResp
	for i := 0; i < 2; i++ {
		events, err := readStream(t, h.client, ctx, streamRequest())
		require.NoError(t, err)
		for _, event := range events {
			require.NotContains(t, protojson.Format(event), "PRIVATE")
		}
		final := events[len(events)-2].GetFinal()
		if first == nil {
			first = final
		} else {
			require.True(t, proto.Equal(first, final))
		}
	}
}
