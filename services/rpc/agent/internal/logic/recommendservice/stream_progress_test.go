package recommendservicelogic

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestStreamProgressBoundsAreStickyAndNotRetryable(t *testing.T) {
	for _, mode := range []string{"chunk", "answer", "events", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			sent := 0
			p := newStreamProgress(func(*pb.RecommendStreamEvent) error { sent++; return nil })
			event := agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: "x"}
			switch mode {
			case "chunk":
				event.Text = strings.Repeat("x", streamcontract.MaxDeltaBytes+1)
			case "answer":
				p.answerBytes = streamcontract.MaxAnswerBytes
			case "events":
				p.events = streamcontract.MaxProgressEvents
			case "bytes":
				p.bytes = streamcontract.MaxProgressBytes
			}
			err := p.Emit(context.Background(), event)
			require.ErrorIs(t, err, agentcore.ErrStreamInterrupted)
			require.ErrorIs(t, err, agentcore.ErrStreamLimit)
			require.Equal(t, codes.ResourceExhausted, status.Code(err))
			require.ErrorIs(t, p.Err(), agentcore.ErrStreamLimit)
			require.False(t, publicRecommendStreamError(mapRecommendError(err)).Retryable)
			require.Error(t, p.Emit(context.Background(), agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: "valid"}))
			require.Zero(t, sent)
		})
	}
}

func TestStreamProgressCorrelatesAndSanitizesTools(t *testing.T) {
	var frames []*pb.RecommendStreamEvent
	p := newStreamProgress(func(f *pb.RecommendStreamEvent) error { frames = append(frames, f); return nil })
	ctx := context.Background()
	start := agentcore.Progress{Kind: streamcontract.ToolStarted, CallID: "tool-1", ToolName: "tool.PRIVATE", Status: "running"}
	require.NoError(t, p.Emit(ctx, start))
	complete := start
	complete.Kind, complete.Status, complete.DurationMS, complete.ErrorCode = streamcontract.ToolCompleted, "failed", 12, "PRIVATE raw error"
	require.NoError(t, p.Emit(ctx, complete))
	require.Equal(t, frames[0].GetTool().Name, frames[1].GetTool().Name)
	require.Equal(t, "execution_failed", frames[1].GetTool().ErrorCode)
	for _, frame := range frames {
		require.NotContains(t, protojson.Format(frame), "PRIVATE")
	}
	require.Error(t, p.Emit(ctx, complete), "a completion cannot be repeated")
	require.Len(t, frames, 2)
}

func TestStreamProgressRejectsMalformedEvents(t *testing.T) {
	for _, event := range []agentcore.Progress{
		{Kind: "PRIVATE"}, {Kind: streamcontract.AnswerDelta}, {Kind: streamcontract.AnswerDelta, Text: string([]byte{0xff})},
		{Kind: streamcontract.AnswerDelta, Text: "text", CallID: "PRIVATE"},
		{Kind: streamcontract.ToolStarted, CallID: "PRIVATE", ToolName: "read_file", Status: "running"},
		{Kind: streamcontract.ToolStarted, CallID: "tool-1", ToolName: "read_file", Status: "PRIVATE"},
		{Kind: streamcontract.ToolCompleted, CallID: "tool-1", ToolName: "read_file", Status: "succeeded"},
		{Kind: streamcontract.ToolStarted, CallID: "tool-1", ToolName: "read_file", Status: "running", Text: "PRIVATE arguments"},
	} {
		p := newStreamProgress(func(*pb.RecommendStreamEvent) error { t.Fatal("invalid event sent"); return nil })
		err := p.Emit(context.Background(), event)
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
		require.NotContains(t, err.Error(), "PRIVATE")
		require.Error(t, p.Err())
	}
}

func TestStreamProgressConcurrentBackpressureAndCloseBarrier(t *testing.T) {
	var active, maximum, sent atomic.Int32
	p := newStreamProgress(func(*pb.RecommendStreamEvent) error {
		n := active.Add(1)
		if n > maximum.Load() {
			maximum.Store(n)
		}
		sent.Add(1)
		active.Add(-1)
		return nil
	})
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- p.Emit(context.Background(), agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: "文"})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), maximum.Load())
	p.Close()
	require.ErrorIs(t, p.Emit(context.Background(), agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: "late"}), agentcore.ErrStreamInterrupted)
	require.Equal(t, int32(64), sent.Load())
}

func TestStreamProgressCanceledWaiterAndSendFailure(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	sendErr := errors.New("transport stopped")
	p := newStreamProgress(func(*pb.RecommendStreamEvent) error { close(started); <-release; return sendErr })
	first := make(chan error, 1)
	go func() {
		first <- p.Emit(context.Background(), agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: "one"})
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.Emit(ctx, agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: "two"}), context.Canceled)
	close(release)
	require.ErrorIs(t, <-first, sendErr)
	require.ErrorIs(t, p.Err(), sendErr)
	require.ErrorIs(t, p.Emit(context.Background(), agentcore.Progress{Kind: streamcontract.AnswerDelta, Text: "three"}), sendErr)
}
