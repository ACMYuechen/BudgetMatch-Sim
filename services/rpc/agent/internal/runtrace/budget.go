// Package runtrace holds bounded, request-local stream budgets and metadata.
// It never retains prompts, model text, tool arguments, credentials or result bodies.
package runtrace

import (
	"context"
	"time"

	"budgetmatch-sim/services/rpc/agent/streamcontract"
)

type budgetKey struct{}
type budget struct{ generation time.Time }

// StreamContext preserves time for finalization/save and terminal transport.
// Shorter caller deadlines proportionally reduce the reserves. These are
// cooperative deadlines, not a way to kill a dependency that ignores context.
func StreamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	now := time.Now()
	end := now.Add(streamcontract.MaxDuration)
	if caller, ok := ctx.Deadline(); ok && caller.Before(end) {
		end = caller
	}
	generation, completion := splitDeadline(now, end)
	ctx = context.WithValue(ctx, budgetKey{}, budget{generation: generation})
	return context.WithDeadline(ctx, completion)
}

func splitDeadline(now, end time.Time) (generation, completion time.Time) {
	remaining := max(end.Sub(now), 0)
	transport := min(250*time.Millisecond, remaining/20)
	finalize := min(3*time.Second, remaining/5)
	completion = end.Add(-transport)
	return completion.Add(-finalize), completion
}

// GenerationContext must be derived from the locked context, retaining store
// connection/lease values. Unary requests without a stream budget are unchanged.
func GenerationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if b, ok := ctx.Value(budgetKey{}).(budget); ok {
		return context.WithDeadline(ctx, b.generation)
	}
	return ctx, func() {}
}
