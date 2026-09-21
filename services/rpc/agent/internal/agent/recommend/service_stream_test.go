package recommend

import (
	"context"
	"errors"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"github.com/stretchr/testify/require"
)

type streamServiceProbe struct {
	runs, streams   int
	result          *agentcore.Result
	failure         error
	ignoreEmitError bool
}

func (*streamServiceProbe) Name() string { return "stream-test" }
func (p *streamServiceProbe) Run(context.Context, agentcore.Input) (*agentcore.Result, error) {
	p.runs++
	return &agentcore.Result{Summary: "unary"}, nil
}
func (p *streamServiceProbe) RunStream(ctx context.Context, _ agentcore.Input, sink agentcore.ProgressSink) (*agentcore.Result, error) {
	p.streams++
	err := sink.Emit(ctx, agentcore.Progress{Kind: "answer.delta", Text: "provisional"})
	if err != nil && !p.ignoreEmitError {
		return nil, err
	}
	return p.result, p.failure
}

type streamServiceSink struct{ failure error }

func (s *streamServiceSink) Emit(context.Context, agentcore.Progress) error { return s.failure }
func (s *streamServiceSink) Err() error                                     { return s.failure }

func TestServiceStreamIsOptInAndCannotFallbackAfterProgress(t *testing.T) {
	for _, mode := range []string{"upstream error", "unsafe result", "ignored emit error"} {
		t.Run(mode, func(t *testing.T) {
			primary, fallback := &streamServiceProbe{result: &agentcore.Result{}}, &streamServiceProbe{}
			sink := &streamServiceSink{}
			switch mode {
			case "upstream error":
				primary.failure = errors.New("stream failed")
			case "unsafe result":
				primary.result = &agentcore.Result{TotalPriceCents: 999999}
			case "ignored emit error":
				sink.failure = agentcore.ErrStreamLimit
				primary.ignoreEmitError = true
			}
			mem := memory.NewInMemory(memory.Conf{})
			service := NewService(fallback, primary, mem)
			input := agentcore.Input{Query: "desk", BudgetCents: 1000, MaxItems: 1, UserId: "u", ConversationId: "c", TurnId: "stream"}
			_, err := service.RecommendStream(context.Background(), input, func(context.Context, string, string) error { return nil }, sink)
			require.ErrorIs(t, err, agentcore.ErrStreamInterrupted)
			require.Equal(t, 1, primary.streams)
			require.Zero(t, primary.runs)
			require.Zero(t, fallback.runs)
			_, found, err := mem.FindTurn(context.Background(), "u", "c", "stream")
			require.NoError(t, err)
			require.False(t, found)
			input.TurnId = "unary"
			_, err = service.Recommend(context.Background(), input)
			require.NoError(t, err)
			require.Equal(t, 1, primary.runs)
			require.Equal(t, 1, primary.streams)
		})
	}
}
