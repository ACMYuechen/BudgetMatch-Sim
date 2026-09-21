package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/streamcontract"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type boundedModelStub struct {
	calls  int
	chunks []*schema.Message
}

func (m *boundedModelStub) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *boundedModelStub) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("Generate forbidden")
}
func (m *boundedModelStub) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	return schema.StreamReaderFromArray(m.chunks), nil
}
func streamFinished(reason string) *schema.Message {
	return &schema.Message{ResponseMeta: &schema.ResponseMeta{FinishReason: reason}}
}

func TestPrivateStreamClassificationHandlesLateToolsAndCompletion(t *testing.T) {
	tool := schema.AssistantMessage("", []schema.ToolCall{{ID: "private", Type: "function", Function: schema.FunctionCall{Name: "search_products", Arguments: "{}"}}})
	for _, tc := range []struct {
		name         string
		messages     []*schema.Message
		tools, valid bool
	}{
		{"late tool", []*schema.Message{schema.AssistantMessage("private reasoning", nil), tool, streamFinished("tool_calls")}, true, true},
		{"final", []*schema.Message{schema.AssistantMessage("private final", nil), streamFinished("stop")}, false, true},
		{"missing finish", []*schema.Message{tool}, false, false},
		{"wrong finish", []*schema.Message{tool, streamFinished("stop")}, false, false},
		{"duplicate finish", []*schema.Message{streamFinished("stop"), streamFinished("stop")}, false, false},
		{"late text", []*schema.Message{streamFinished("stop"), schema.AssistantMessage("private late", nil)}, false, false},
		{"nil", []*schema.Message{nil}, false, false},
		{"empty", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inspectToolStream(context.Background(), schema.StreamReaderFromArray(tc.messages))
			require.Equal(t, tc.valid, err == nil)
			require.Equal(t, tc.tools, got)
			if err != nil {
				require.NotContains(t, err.Error(), "private")
			}
		})
	}
}

func TestBoundedStreamModelCountsCallsAndContextBeforeProvider(t *testing.T) {
	base := &boundedModelStub{chunks: []*schema.Message{streamFinished("stop")}}
	bounded := newBoundedStreamModel(base, 1000)
	bound, err := bounded.WithTools(nil)
	require.NoError(t, err)
	for i := 0; i < streamcontract.MaxModelCalls; i++ {
		source, err := bound.Stream(context.Background(), nil)
		require.NoError(t, err)
		source.Close()
	}
	_, err = bounded.Stream(context.Background(), nil)
	require.ErrorIs(t, err, agentcore.ErrStreamLimit)
	require.Equal(t, streamcontract.MaxModelCalls, base.calls)
	_, err = newBoundedStreamModel(base, 1).Stream(context.Background(), []*schema.Message{schema.SystemMessage("oversized context")})
	require.ErrorIs(t, err, agentcore.ErrContextTooLarge)
	require.Equal(t, streamcontract.MaxModelCalls, base.calls)
	withLargeTools, err := newBoundedStreamModel(base, 1000).WithTools([]*schema.ToolInfo{{Name: "tool", Desc: strings.Repeat("x", 10000)}})
	require.NoError(t, err)
	_, err = withLargeTools.Stream(context.Background(), nil)
	require.ErrorIs(t, err, agentcore.ErrContextTooLarge, "tool schemas count toward every model call's context budget")
	require.Equal(t, streamcontract.MaxModelCalls, base.calls)
	_, err = bounded.Generate(context.Background(), nil)
	require.Error(t, err)
}

type toolProgressProbe struct {
	events []agentcore.Progress
	failAt string
	err    error
}

func (p *toolProgressProbe) Emit(_ context.Context, event agentcore.Progress) error {
	p.events = append(p.events, event)
	if event.Kind == p.failAt {
		p.err = errors.New("PRIVATE send failure")
	}
	return p.err
}
func (p *toolProgressProbe) Err() error { return p.err }

func TestStreamingToolEventsWrapActualExecutionAndSafeFailure(t *testing.T) {
	for _, mode := range []string{"success", "tool error", "invalid arguments", "start send", "complete send", "tool limit"} {
		t.Run(mode, func(t *testing.T) {
			progress := &toolProgressProbe{}
			s := &session{progress: progress}
			base := &securityTool{output: "PRIVATE tool body"}
			arguments := `{"path":"PRIVATE path"}`
			switch mode {
			case "tool error":
				base.err = errors.New("PRIVATE tool failure")
			case "invalid arguments":
				arguments = "PRIVATE invalid json"
			case "start send":
				progress.failAt = streamcontract.ToolStarted
			case "complete send":
				progress.failAt = streamcontract.ToolCompleted
			case "tool limit":
				s.progressCalls.Store(streamcontract.MaxToolCalls)
			}
			out, err := decorate(s, toolReadFile, base).(tool.InvokableTool).InvokableRun(context.Background(), arguments)
			if mode == "start send" || mode == "complete send" {
				require.ErrorIs(t, err, agentcore.ErrStreamInterrupted)
				require.True(t, agentcore.IsExecutionStopped(err))
				require.Empty(t, out, "delivery failure cannot become recoverable tool feedback")
			} else if mode == "tool limit" {
				require.ErrorIs(t, err, agentcore.ErrStreamLimit)
				require.Empty(t, progress.events)
			} else {
				require.NoError(t, err)
				require.Len(t, progress.events, 2)
				require.Equal(t, "tool-1", progress.events[0].CallID)
				require.Equal(t, progress.events[0].CallID, progress.events[1].CallID)
				require.Equal(t, "running", progress.events[0].Status)
				if mode == "success" {
					require.Equal(t, "succeeded", progress.events[1].Status)
				} else {
					require.Equal(t, "failed", progress.events[1].Status)
					require.NotEmpty(t, progress.events[1].ErrorCode)
				}
			}
			wantCalls := 1
			if mode == "start send" || mode == "invalid arguments" || mode == "tool limit" {
				wantCalls = 0
			}
			require.Equal(t, wantCalls, base.calls)
			encoded, err := json.Marshal(progress.events)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "PRIVATE")
		})
	}
}

func TestBoundedStreamModelRejectsUnboundedOrMalformedChunks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []*schema.Message
		code   codes.Code
	}{
		{"private reasoning size", []*schema.Message{{ReasoningContent: strings.Repeat("x", streamcontract.MaxModelBytes+1)}}, codes.ResourceExhausted},
		{"private metadata size", []*schema.Message{{Extra: map[string]any{"private": strings.Repeat("x", streamcontract.MaxModelBytes+1)}}}, codes.ResourceExhausted},
		{"nil", []*schema.Message{nil}, codes.FailedPrecondition},
		{"tool role", []*schema.Message{schema.ToolMessage("private", "call")}, codes.FailedPrecondition},
		{"truncation", []*schema.Message{streamFinished("length")}, codes.FailedPrecondition},
		{"many empty chunks", make([]*schema.Message, streamcontract.MaxModelChunks+1), codes.ResourceExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "many empty chunks" {
				for i := range tc.chunks {
					tc.chunks[i] = &schema.Message{}
				}
			}
			source, err := newBoundedStreamModel(&boundedModelStub{chunks: tc.chunks}, 1000).Stream(context.Background(), nil)
			require.NoError(t, err)
			defer source.Close()
			for {
				_, err = source.Recv()
				if err != nil {
					break
				}
			}
			require.NotErrorIs(t, err, io.EOF)
			require.Equal(t, tc.code, status.Code(err))
			require.NotContains(t, err.Error(), "private")
		})
	}
}
