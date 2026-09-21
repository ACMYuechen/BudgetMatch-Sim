package llm

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/runtrace"
	"budgetmatch-sim/services/rpc/agent/streamcontract"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func usageMessage(prompt, completion int) *schema.Message {
	return &schema.Message{ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
		PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion,
	}}}
}

func TestBoundedStreamUsageTracksEOFAndWithToolsWithoutDuplication(t *testing.T) {
	for _, mode := range []string{"normal", "missing", "early close", "invalid", "source error", "canceled EOF"} {
		t.Run(mode, func(t *testing.T) {
			base := &usageStreamSource{chunks: []*schema.Message{usageMessage(10, 2), streamFinished("stop"), usageMessage(10, 2)}}
			switch mode {
			case "missing":
				base.chunks = []*schema.Message{streamFinished("stop")}
			case "invalid":
				base.chunks[2] = usageMessage(-1, 2)
			case "source error":
				base.err = errors.New("PRIVATE source")
			}
			ctx, recorder := runtrace.Start(context.Background(), "execution")
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			bound := newBoundedStreamModel(base, 1000)
			copy, err := bound.WithTools(nil)
			require.NoError(t, err)
			source, err := copy.Stream(ctx, nil)
			require.NoError(t, err)
			if mode == "early close" {
				_, err = source.Recv()
				require.NoError(t, err)
			} else {
				for range base.chunks {
					_, err = source.Recv()
					require.NoError(t, err)
				}
				if mode == "canceled EOF" {
					cancel()
				}
				_, err = source.Recv()
				if mode == "source error" || mode == "canceled EOF" {
					require.Error(t, err)
					require.NotErrorIs(t, err, io.EOF)
				} else {
					require.ErrorIs(t, err, io.EOF)
				}
			}
			source.Close()
			got := recorder.Finish(err, false)
			require.Equal(t, 1, got.ModelCalls)
			if mode == "normal" {
				require.Equal(t, "complete", got.UsageStatus)
				require.Equal(t, &runtrace.Usage{Prompt: 10, Completion: 2, Total: 12}, got.ReportedUsage)
			} else {
				require.Equal(t, "unknown", got.UsageStatus)
				require.Nil(t, got.ReportedUsage)
			}
		})
	}
}

type usageStreamSource struct {
	model.ToolCallingChatModel
	chunks []*schema.Message
	err    error
}

func (s *usageStreamSource) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}
func (s *usageStreamSource) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	source := schema.StreamReaderFromArray(s.chunks)
	if s.err == nil {
		return source, nil
	}
	return schema.StreamReaderWithConvert(source, func(m *schema.Message) (*schema.Message, error) { return m, nil }, schema.WithOnEOF(func() (any, error) { return nil, s.err })), nil
}

func TestBoundedStreamWholeAttemptEstimateRejectsBeforeProvider(t *testing.T) {
	ctx, recorder := runtrace.Start(context.Background(), "execution")
	base := &boundedModelStub{chunks: []*schema.Message{streamFinished("stop")}}
	bound := newBoundedStreamModel(base, 20000)
	copy, err := bound.WithTools(nil)
	require.NoError(t, err)
	input := []*schema.Message{schema.UserMessage(strings.Repeat("中", 16000))}
	for i := 0; i < 3; i++ {
		source, err := copy.Stream(ctx, input)
		require.NoError(t, err)
		source.Close()
	}
	_, err = bound.Stream(ctx, input)
	require.ErrorIs(t, err, agentcore.ErrStreamLimit)
	require.Equal(t, 3, base.calls, "WithTools shares the full-attempt estimate reservation")
	require.Equal(t, 3, recorder.Snapshot().ModelCalls)
	require.LessOrEqual(t, recorder.Snapshot().EstimatedTokens, int64(streamcontract.MaxEstimatedTokens))
	_, err = newBoundedStreamModel(base, 1000).Stream(ctx, nil, model.WithMaxTokens(-1))
	require.ErrorIs(t, err, agentcore.ErrStreamLimit)
	require.Equal(t, 3, base.calls)
}
