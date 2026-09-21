package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type gatewayStream struct {
	grpc.ClientStream
	frames     []*pb.RecommendStreamEvent
	err        error
	received   int
	beforeRecv func()
}

func (s *gatewayStream) Recv() (*pb.RecommendStreamEvent, error) {
	if s.beforeRecv != nil {
		s.beforeRecv()
	}
	s.received++
	if len(s.frames) > 0 {
		frame := s.frames[0]
		s.frames = s.frames[1:]
		return frame, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	return nil, io.EOF
}

type gatewayClient struct {
	recommendservice.RecommendService
	source         *gatewayStream
	ctx            context.Context
	input          *pb.RecommendReq
	err            error
	streams, unary int
}

func (c *gatewayClient) RecommendStream(ctx context.Context, in *pb.RecommendReq, _ ...grpc.CallOption) (pb.RecommendService_RecommendStreamClient, error) {
	c.streams++
	c.ctx, c.input = ctx, in
	if c.err != nil {
		return nil, c.err
	}
	return c.source, nil
}
func (c *gatewayClient) Recommend(context.Context, *pb.RecommendReq, ...grpc.CallOption) (*pb.RecommendResp, error) {
	c.unary++
	return gatewayFinal(), nil
}
func gatewayFinal() *pb.RecommendResp {
	return &pb.RecommendResp{ConversationId: "c", TurnId: "t", Summary: "validated", Intent: &pb.Intent{BudgetCents: 1000, MaxItems: 1}}
}
func gatewayReq() *types.AgentRecommendReq {
	return &types.AgentRecommendReq{ConversationId: "c", TurnId: "t", Query: "PRIVATE user query", BudgetCents: 1000, MaxItems: 1}
}
func gatewayFrames() []*pb.RecommendStreamEvent {
	frames := []*pb.RecommendStreamEvent{
		{Event: streamcontract.Accepted, Payload: &pb.RecommendStreamEvent_Accepted{Accepted: &pb.StreamAccepted{}}},
		{Event: streamcontract.ToolStarted, Payload: &pb.RecommendStreamEvent_Tool{Tool: &pb.StreamToolEvent{CallId: "tool-1", Name: "tool.search_products", Status: "running"}}},
		{Event: streamcontract.ToolCompleted, Payload: &pb.RecommendStreamEvent_Tool{Tool: &pb.StreamToolEvent{CallId: "tool-1", Name: "tool.search_products", Status: "succeeded", DurationMs: 1}}},
		{Event: streamcontract.AnswerDelta, Payload: &pb.RecommendStreamEvent_AnswerDelta{AnswerDelta: &pb.StreamAnswerDelta{Text: "临时解释", Provisional: true}}},
		{Event: streamcontract.Final, Payload: &pb.RecommendStreamEvent_Final{Final: gatewayFinal()}},
		{Event: streamcontract.Done, Payload: &pb.RecommendStreamEvent_Done{Done: &pb.StreamDone{Ok: true}}},
	}
	stampGatewayFrames(frames)
	return frames
}
func stampGatewayFrames(frames []*pb.RecommendStreamEvent) {
	for i, f := range frames {
		f.SchemaVersion, f.ExecutionId, f.ConversationId, f.TurnId, f.Sequence = 1, "execution", "c", "t", uint64(i+1)
	}
}

func TestGatewayStreamProgressAndTerminalEOF(t *testing.T) {
	for _, mode := range []string{"new", "replay", "unknown", "business error"} {
		t.Run(mode, func(t *testing.T) {
			frames := gatewayFrames()
			if mode == "replay" {
				frames = frames[4:]
				frames[1].GetDone().Replayed = true
			}
			if mode == "unknown" {
				frames = append(frames[:4], append([]*pb.RecommendStreamEvent{{Event: "future.progress"}}, frames[4:]...)...)
			}
			if mode == "business error" {
				frames[4] = &pb.RecommendStreamEvent{Event: streamcontract.Error, Payload: &pb.RecommendStreamEvent_Error{Error: &pb.StreamError{Code: 500000, Message: "公共错误"}}}
				frames[5].GetDone().Ok = false
			}
			stampGatewayFrames(frames)
			source := &gatewayStream{frames: frames}
			client := &gatewayClient{source: source}
			ctx := context.WithValue(request.WithUserId(context.Background(), "trusted"), "token", "test-token")
			var events []*types.AgentStreamEvent
			err := NewAgentRecommendStreamLogic(ctx, &svc.ServiceContext{AgentClient: client}).AgentRecommendStream(gatewayReq(), func(event StreamEvent) error {
				frame := event.Data.(*types.AgentStreamEvent)
				require.NotEmpty(t, event.ID)
				if frame.Final != nil || frame.Done != nil {
					require.Greater(t, source.received, len(frames), "terminal pair must wait for EOF")
				}
				events = append(events, frame)
				return nil
			})
			require.NoError(t, err)
			require.Len(t, events, len(frames))
			for i, event := range events {
				require.Equal(t, uint64(i+1), event.Sequence)
			}
			require.Equal(t, "test-token", client.ctx.Value("token"))
			deadline, ok := client.ctx.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), streamcontract.MaxDuration)
			require.ErrorIs(t, client.ctx.Err(), context.Canceled)
			require.Equal(t, 1, client.streams)
			require.Zero(t, client.unary)
		})
	}
}

func TestGatewayStreamMalformedOrInterruptedNeverForwardsFinal(t *testing.T) {
	for _, mode := range []string{"missing done", "after done", "late error", "open error", "version", "sequence", "identity", "execution", "payload", "delta limit", "tool orphan", "raw tool name", "unknown completion", "nil", "many events"} {
		t.Run(mode, func(t *testing.T) {
			source := &gatewayStream{frames: gatewayFrames()}
			client := &gatewayClient{source: source}
			switch mode {
			case "missing done":
				source.frames = source.frames[:5]
			case "after done":
				source.frames = append(source.frames, source.frames[0])
			case "late error":
				source.err = status.Error(codes.Unavailable, "PRIVATE upstream")
			case "open error":
				client.err = status.Error(codes.Unimplemented, "PRIVATE unsupported")
			case "version":
				source.frames[3].SchemaVersion = 2
			case "sequence":
				source.frames[3].Sequence++
			case "identity":
				source.frames[3].TurnId = "PRIVATE"
			case "execution":
				source.frames[3].ExecutionId = "PRIVATE-other"
			case "payload":
				source.frames[3].Event = streamcontract.Final
			case "delta limit":
				source.frames[3].GetAnswerDelta().Text = strings.Repeat("x", 2049)
			case "tool orphan":
				source.frames[2].GetTool().CallId = "tool-2"
			case "raw tool name":
				source.frames[1].GetTool().Name = "PRIVATE"
			case "unknown completion":
				source.frames[5].GetDone().Replayed = true
			case "nil":
				source.frames[3] = nil
			case "many events":
				source.frames = source.frames[:1]
				for i := 0; i <= streamcontract.MaxProgressEvents; i++ {
					source.frames = append(source.frames, &pb.RecommendStreamEvent{Event: "future.event"})
				}
				stampGatewayFrames(source.frames)
			}
			var events []*types.AgentStreamEvent
			err := NewAgentRecommendStreamLogic(request.WithUserId(context.Background(), "u"), &svc.ServiceContext{AgentClient: client}).AgentRecommendStream(gatewayReq(), func(event StreamEvent) error {
				events = append(events, event.Data.(*types.AgentStreamEvent))
				return nil
			})
			require.Error(t, err)
			require.GreaterOrEqual(t, len(events), 2)
			require.NotNil(t, events[len(events)-2].Error)
			require.False(t, events[len(events)-1].Done.Ok)
			for i, event := range events {
				require.Nil(t, event.Final)
				require.Equal(t, uint64(i+1), event.Sequence)
				body, err := json.Marshal(event)
				require.NoError(t, err)
				require.NotContains(t, string(body), "PRIVATE")
			}
			require.Zero(t, client.unary, "Unimplemented/partial failures must never trigger a second execution")
		})
	}
}

func TestGatewayStreamEmitFailureAndCancellationCloseRPC(t *testing.T) {
	for _, mode := range []string{"emit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(request.WithUserId(context.Background(), "u"))
			defer cancel()
			source := &gatewayStream{frames: gatewayFrames()}
			client := &gatewayClient{source: source}
			if mode == "cancel" {
				source.beforeRecv = cancel
			}
			sends := 0
			err := NewAgentRecommendStreamLogic(ctx, &svc.ServiceContext{AgentClient: client}).AgentRecommendStream(gatewayReq(), func(StreamEvent) error { sends++; return errors.New("transport failure") })
			require.Error(t, err)
			require.Equal(t, 1, source.received)
			require.ErrorIs(t, client.ctx.Err(), context.Canceled)
			if mode == "cancel" {
				require.Zero(t, sends)
			} else {
				require.Equal(t, 1, sends)
			}
		})
	}
}

func TestGatewayStreamTrustedIdentityAndEarlyValidation(t *testing.T) {
	client := &gatewayClient{}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), "user_id", "forged")} {
		err := NewAgentRecommendStreamLogic(ctx, &svc.ServiceContext{AgentClient: client}).AgentRecommendStream(gatewayReq(), func(StreamEvent) error { t.Fatal("unauthorized event"); return nil })
		require.ErrorIs(t, err, apperrors.Unauthorized)
	}
	for _, field := range []string{"max items", "query utf8", "id utf8"} {
		in := gatewayReq()
		switch field {
		case "max items":
			in.MaxItems = 1 << 40
		case "query utf8":
			in.Query = string([]byte{0xff})
		case "id utf8":
			in.TurnId = string([]byte{0xff})
		}
		err := NewAgentRecommendStreamLogic(request.WithUserId(context.Background(), "u"), &svc.ServiceContext{AgentClient: client}).AgentRecommendStream(in, func(StreamEvent) error { return nil })
		require.ErrorIs(t, err, apperrors.Invalid, field)
	}
	require.Zero(t, client.streams)
}

func TestGatewayPublicStreamErrorsNeverExposeUnknownDetails(t *testing.T) {
	for _, err := range []error{errors.New("PRIVATE transport body"), status.Error(codes.Internal, "PRIVATE upstream body"), apperrors.Invalid} {
		response := publicStreamError(err).(apperrors.HTTPResponse)
		require.GreaterOrEqual(t, int(response.Code), 400000)
		require.NotContains(t, response.Message, "PRIVATE")
	}
}
