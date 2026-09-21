package recommendservicelogic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/infra/interceptor"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/llm"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	logic "budgetmatch-sim/services/rpc/agent/internal/logic/recommendservice"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Each call produces its own pipe. The final explanation can pause between
// chunks, proving visible output precedes model completion (not Generate cuts).
type modelStreamScript struct {
	mu        sync.Mutex
	calls     int
	generate  atomic.Int32
	answer    []*schema.Message
	answerErr error
	gate      <-chan struct{}
	closed    chan struct{}
	inputs    [][]*schema.Message
	options   []*model.Options
	usage     bool
}

type modelStreamStub struct {
	script *modelStreamScript
	bound  []*schema.ToolInfo
}

func (m *modelStreamStub) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &modelStreamStub{script: m.script, bound: append([]*schema.ToolInfo(nil), infos...)}, nil
}
func (m *modelStreamStub) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.script.generate.Add(1)
	return nil, errors.New("unexpected Generate")
}

func finishedMessage(reason string) *schema.Message {
	return &schema.Message{ResponseMeta: &schema.ResponseMeta{FinishReason: reason}}
}

func (m *modelStreamStub) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	s := m.script
	s.mu.Lock()
	index := s.calls
	s.calls++
	s.inputs = append(s.inputs, input)
	s.options = append(s.options, model.GetCommonOptions(&model.Options{Tools: m.bound}, opts...))
	s.mu.Unlock()
	var chunks []*schema.Message
	switch index {
	case 0:
		// Late tool call after private prose: the default first-content checker
		// would misclassify this; our bounded orchestration checker must not.
		chunks = []*schema.Message{schema.AssistantMessage("PRIVATE orchestration preface", nil),
			schema.AssistantMessage("", []schema.ToolCall{{ID: "PRIVATE-model-call-id", Type: "function", Function: schema.FunctionCall{Name: "search_products",
				Arguments: `{"query":"study","budget_cents":300000,"max_items":2}`}}}), finishedMessage("tool_calls")}
	case 1:
		chunks = []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: "PRIVATE-select-id", Type: "function", Function: schema.FunctionCall{Name: "select_bundle",
			Arguments: `{"budget_cents":300000,"max_items":2}`}}}), finishedMessage("tool_calls")}
	case 2:
		chunks = []*schema.Message{{Role: schema.Assistant, Content: "PRIVATE final orchestration text", ReasoningContent: "PRIVATE reasoning", Extra: map[string]any{"debug": "PRIVATE"}}, finishedMessage("stop")}
	case 3:
		chunks = s.answer
	default:
		return nil, errors.New("model was rerun unexpectedly")
	}
	if s.usage {
		chunks = append(append([]*schema.Message(nil), chunks...), &schema.Message{ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}}})
	}
	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer writer.Close()
		if index == 3 && s.closed != nil {
			defer close(s.closed)
		}
		for i, chunk := range chunks {
			if index == 3 && i == 1 && s.gate != nil {
				select {
				case <-s.gate:
				case <-ctx.Done():
					return
				}
			}
			if writer.Send(chunk, nil) {
				return
			}
		}
		if index == 3 && s.answerErr != nil {
			writer.Send(nil, s.answerErr)
		}
	}()
	return reader, nil
}

func newModelStreamService(s *modelStreamScript, store *streamStore, fallback *streamAgent, finalizer *streamFinalizer) *recommend.Service {
	runner := llm.NewAgent(&modelStreamStub{script: s}, tools.NewMockProductProvider(), selector.NewBundleSelector(), mcp.Config{}, filetools.Config{}).
		WithMaxContextTokens(32000)
	return recommend.NewService(fallback, runner, store).WithFinalizer(finalizer)
}

func answerChunks() []*schema.Message {
	return []*schema.Message{
		{Role: schema.Assistant, Content: "已按预算整理。", ReasoningContent: "PRIVATE hidden reasoning", Extra: map[string]any{"debug": "PRIVATE metadata"}},
		{Content: "请以最终方案为准。"}, finishedMessage("stop"),
	}
}

func TestRecommendStreamEinoIncrementalToolsAndReplay(t *testing.T) {
	gate := make(chan struct{})
	script := &modelStreamScript{answer: answerChunks(), gate: gate, closed: make(chan struct{})}
	store, fallback, finalizer := newStreamStore(), &streamAgent{}, &streamFinalizer{}
	h := newStreamHarness(t, newModelStreamService(script, store, fallback, finalizer))
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	req := streamRequest()
	req.Query, req.BudgetCents = "PRIVATE user query", 300000
	stream, err := h.client.RecommendStream(ctx, req)
	require.NoError(t, err)
	var events []*pb.RecommendStreamEvent
	for {
		event, err := stream.Recv()
		require.NoError(t, err)
		events = append(events, event)
		if event.Event == streamcontract.AnswerDelta {
			break
		}
		require.Less(t, len(events), 10)
	}
	require.Zero(t, store.saves.Load(), "first delta must precede save")
	require.Zero(t, finalizer.calls.Load(), "explanation is provisional, not live checked yet")
	select {
	case <-script.closed:
		t.Fatal("answer was fully produced before first public delta")
	default:
	}
	close(gate)
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		events = append(events, event)
	}
	assertStreamEnvelope(t, events, streamcontract.Accepted, streamcontract.ToolStarted, streamcontract.ToolCompleted,
		streamcontract.ToolStarted, streamcontract.ToolCompleted, streamcontract.AnswerDelta, streamcontract.AnswerDelta, streamcontract.Final, streamcontract.Done)
	require.Equal(t, "tool-1", events[1].GetTool().CallId)
	require.Equal(t, events[1].GetTool().CallId, events[2].GetTool().CallId)
	require.Equal(t, "tool.search_products", events[1].GetTool().Name)
	require.Equal(t, "running", events[1].GetTool().Status)
	require.Equal(t, "succeeded", events[2].GetTool().Status)
	require.NotEqual(t, events[1].GetTool().CallId, events[3].GetTool().CallId)
	for _, event := range events {
		if event.Event != streamcontract.Final {
			require.NotContains(t, protojson.Format(event), "PRIVATE")
		}
	}
	final := events[7].GetFinal()
	require.NotEmpty(t, final.Items)
	require.NotContains(t, final.Summary, "已按预算整理")
	turn, found, err := store.FindTurn(ctx, "user", req.ConversationId, req.TurnId)
	require.NoError(t, err)
	require.True(t, found)
	require.NotContains(t, string(turn.ResultJSON), "请以最终方案为准")
	require.Equal(t, int32(1), store.saves.Load())
	require.Equal(t, int32(1), finalizer.calls.Load())
	require.Zero(t, fallback.calls.Load())
	replay, err := readStream(t, h.client, ctx, req)
	require.NoError(t, err)
	assertStreamEnvelope(t, replay, streamcontract.Final, streamcontract.Done)
	require.True(t, proto.Equal(final, replay[0].GetFinal()))
	unary, err := h.client.Recommend(ctx, req)
	require.NoError(t, err)
	require.True(t, proto.Equal(final, unary))
	script.mu.Lock()
	defer script.mu.Unlock()
	require.Equal(t, 4, script.calls, "3 orchestration Stream calls plus 1 explanation; replay makes no call")
	require.Zero(t, script.generate.Load())
	require.Len(t, script.inputs[3], 2)
	prompt, err := json.Marshal(script.inputs[3])
	require.NoError(t, err)
	require.NotContains(t, string(prompt), "PRIVATE")
	require.NotContains(t, string(prompt), "search_products")
	var projection map[string]int64
	require.NoError(t, json.Unmarshal([]byte(script.inputs[3][1].Content), &projection))
	require.Len(t, projection, 4)
	require.Equal(t, int64(300000), projection["budget_cents"])
	require.Empty(t, script.options[3].Tools)
	require.Equal(t, schema.ToolChoiceForbidden, *script.options[3].ToolChoice)
	require.Equal(t, 512, *script.options[3].MaxTokens)
}

func TestRecommendStreamEinoCancelClosesProducerWithoutSaveOrFallback(t *testing.T) {
	script := &modelStreamScript{answer: answerChunks(), gate: make(chan struct{}), closed: make(chan struct{})}
	store, fallback, finalizer := newStreamStore(), &streamAgent{}, &streamFinalizer{}
	h := newStreamHarness(t, newModelStreamService(script, store, fallback, finalizer))
	ctx, cancel := streamClientContext(t, "user")
	defer cancel()
	stream, err := h.client.RecommendStream(ctx, streamRequest())
	require.NoError(t, err)
	for {
		event, err := stream.Recv()
		require.NoError(t, err)
		if event.GetAnswerDelta() != nil {
			break
		}
	}
	cancel()
	_, err = stream.Recv()
	require.Equal(t, codes.Canceled, status.Code(err))
	require.Error(t, waitStreamFinished(t, h))
	select {
	case <-script.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled model producer leaked")
	}
	require.Zero(t, store.saves.Load())
	require.Zero(t, finalizer.calls.Load())
	require.Zero(t, fallback.calls.Load())
}

func TestRecommendStreamEinoMalformedOrInterruptedAnswerFailsClosed(t *testing.T) {
	for _, mode := range []string{"late error", "missing finish", "truncated", "tool call", "tool role", "after finish", "oversized", "invalid utf8", "empty", "hidden only"} {
		t.Run(mode, func(t *testing.T) {
			script := &modelStreamScript{answer: answerChunks()}
			switch mode {
			case "late error":
				script.answer = script.answer[:1]
				script.answerErr = status.Error(codes.Unavailable, "PRIVATE upstream")
			case "missing finish":
				script.answer = script.answer[:2]
			case "truncated":
				script.answer[2] = finishedMessage("length")
			case "tool call":
				script.answer[1] = schema.AssistantMessage("PRIVATE tool prefix", []schema.ToolCall{{ID: "PRIVATE", Function: schema.FunctionCall{Name: "write_file", Arguments: "PRIVATE"}}})
			case "tool role":
				script.answer[1] = schema.ToolMessage("PRIVATE body", "PRIVATE")
			case "after finish":
				script.answer = append(script.answer, schema.AssistantMessage("PRIVATE late text", nil))
			case "oversized":
				script.answer[0] = schema.AssistantMessage(strings.Repeat("x", streamcontract.MaxDeltaBytes+1), nil)
			case "invalid utf8":
				script.answer[0] = schema.AssistantMessage(string([]byte{0xff}), nil)
			case "empty":
				script.answer = nil
			case "hidden only":
				script.answer = []*schema.Message{{ReasoningContent: "PRIVATE hidden"}, finishedMessage("stop")}
			}
			store, fallback, finalizer := newStreamStore(), &streamAgent{}, &streamFinalizer{}
			h := newStreamHarness(t, newModelStreamService(script, store, fallback, finalizer))
			ctx, cancel := streamClientContext(t, "user")
			defer cancel()
			events, err := readStream(t, h.client, ctx, streamRequest())
			require.NoError(t, err)
			require.Equal(t, streamcontract.Error, events[len(events)-2].Event)
			require.Equal(t, streamcontract.Done, events[len(events)-1].Event)
			require.False(t, events[len(events)-1].GetDone().Ok)
			for _, event := range events {
				require.Nil(t, event.GetFinal())
				require.NotContains(t, protojson.Format(event), "PRIVATE")
			}
			require.Zero(t, store.saves.Load())
			require.Zero(t, finalizer.calls.Load())
			require.Zero(t, fallback.calls.Load())
			if mode == "oversized" {
				require.False(t, events[len(events)-2].GetError().Retryable)
			}
		})
	}
}

func TestRecommendStreamEinoFinalizationOrSaveFailureAfterDeltas(t *testing.T) {
	for _, stage := range []string{"finalizer", "unsafe finalized result", "save"} {
		t.Run(stage, func(t *testing.T) {
			script := &modelStreamScript{answer: answerChunks()}
			store, fallback, finalizer := newStreamStore(), &streamAgent{}, &streamFinalizer{}
			switch stage {
			case "finalizer":
				finalizer.run = func(context.Context, *agentcore.Result) error {
					return status.Error(codes.Unavailable, "PRIVATE live check")
				}
			case "unsafe finalized result":
				finalizer.run = func(_ context.Context, result *agentcore.Result) error {
					result.TotalPriceCents = result.Intent.BudgetCents + 1
					return nil
				}
			case "save":
				store.save = func(context.Context, memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
					return memory.Conversation{}, memory.Turn{}, errors.New("PRIVATE storage failure")
				}
			}
			h := newStreamHarness(t, newModelStreamService(script, store, fallback, finalizer))
			ctx, cancel := streamClientContext(t, "user")
			defer cancel()
			events, err := readStream(t, h.client, ctx, streamRequest())
			require.NoError(t, err)
			assertStreamEnvelope(t, events, streamcontract.Accepted, streamcontract.ToolStarted, streamcontract.ToolCompleted,
				streamcontract.ToolStarted, streamcontract.ToolCompleted, streamcontract.AnswerDelta, streamcontract.AnswerDelta,
				streamcontract.Error, streamcontract.Done)
			require.False(t, events[len(events)-1].GetDone().Ok)
			require.Equal(t, stage == "finalizer", events[len(events)-2].GetError().Retryable)
			for _, event := range events {
				require.Nil(t, event.GetFinal(), "provisional text cannot authorize a final result")
				require.NotContains(t, protojson.Format(event), "PRIVATE")
			}
			_, saved, err := store.FindTurn(ctx, "user", "conversation", "turn")
			require.NoError(t, err)
			require.False(t, saved)
			require.Equal(t, int32(1), finalizer.calls.Load())
			require.Zero(t, fallback.calls.Load())
			require.Equal(t, 4, script.calls, "failure after explanation must not rerun any model call")
		})
	}
}

func TestRecommendStreamEinoProgressSendFailureStopsExecution(t *testing.T) {
	for _, failAt := range []string{streamcontract.ToolStarted, streamcontract.ToolCompleted, streamcontract.AnswerDelta} {
		t.Run(failAt, func(t *testing.T) {
			script := &modelStreamScript{answer: answerChunks(), closed: make(chan struct{})}
			store, fallback, finalizer := newStreamStore(), &streamAgent{}, &streamFinalizer{}
			service := newModelStreamService(script, store, fallback, finalizer)
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user"), time.Second)
			defer cancel()
			attempts := 0
			fake := &fakeRecommendStream{ctx: ctx, send: func(event *pb.RecommendStreamEvent) error {
				attempts++
				if event.Event == failAt {
					return status.Error(codes.Unavailable, "PRIVATE transport")
				}
				return nil
			}}
			err := logic.NewRecommendStreamLogic(ctx, &svc.ServiceContext{RecommendService: service}).RecommendStream(streamRequest(), fake)
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.NotContains(t, err.Error(), "PRIVATE")
			require.Equal(t, len(fake.events)+1, attempts, "no retry or terminal Send after broken transport")
			require.Zero(t, fallback.calls.Load())
			require.Zero(t, finalizer.calls.Load())
			require.Zero(t, store.saves.Load())
			if failAt == streamcontract.AnswerDelta {
				select {
				case <-script.closed:
				case <-time.After(5 * time.Second):
					t.Fatal("model producer was not closed after progress Send failed")
				}
			}
		})
	}
}

// Compile-time check that the Eino runner, not a hand-written replacement,
// implements the domain opt-in streaming boundary exercised above.
var _ agentcore.StreamingAgent = (*llm.Agent)(nil)
