package agent

import "context"

// Progress is deliberately separate from Result: it never contains an
// actionable bundle, raw tool arguments/results or model reasoning.
type Progress struct {
	Kind       string
	Text       string
	CallID     string
	ToolName   string
	Status     string
	DurationMS int64
	ErrorCode  string
}

// ProgressSink applies request-scoped backpressure. Implementations must be
// safe for concurrent callers; a failed Emit is terminal, not a tool retry.
type ProgressSink interface {
	Emit(context.Context, Progress) error
	// Err retains the first delivery/protocol/budget failure, even if a producer
	// accidentally ignores Emit's error. Service checks it before finalization.
	Err() error
}

// StreamingAgent is opt-in. Unary still calls Run, and rule-only agents need
// not synthesize text deltas to implement the recommendation lifecycle stream.
type StreamingAgent interface {
	Agent
	RunStream(context.Context, Input, ProgressSink) (*Result, error)
}
