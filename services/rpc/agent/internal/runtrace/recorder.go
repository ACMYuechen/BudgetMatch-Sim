package runtrace

import (
	"context"
	"errors"
	"sync"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type contextKey struct{}

// Usage is a provider-reported cumulative snapshot for ONE model call, never a
// delta or a locally estimated invoice. Missing/invalid/incomplete calls remain
// unknown. No provider pricing is configured, so monetary cost stays unknown.
type Usage struct {
	Prompt     int64 `json:"prompt_tokens"`
	Completion int64 `json:"completion_tokens"`
	Total      int64 `json:"total_tokens"`
}

type Summary struct {
	ExecutionID       string `json:"execution_id"`
	Outcome           string `json:"outcome"`
	ErrorCode         string `json:"error_code"`
	Replayed          bool   `json:"replayed"`
	Committed         bool   `json:"committed"`
	Path              string `json:"path,omitempty"`
	FallbackCode      string `json:"fallback_code,omitempty"`
	SelectedProvider  string `json:"selected_provider,omitempty"`
	DurationMS        int64  `json:"duration_ms"`
	LockWaitMS        int64  `json:"lock_wait_ms"`
	GenerationMS      int64  `json:"generation_ms"`
	FinalizationMS    int64  `json:"finalization_ms"`
	SaveMS            int64  `json:"save_ms"`
	FirstDeltaMS      *int64 `json:"first_delta_ms,omitempty"`
	EventsSent        int    `json:"events_sent"`
	ToolStartsSent    int    `json:"tool_starts_sent"`
	AnswerBytesSent   int    `json:"answer_bytes_sent"`
	ModelCalls        int    `json:"model_calls"`
	ModelMS           int64  `json:"model_ms"`
	EstimatedTokens   int64  `json:"reserved_estimated_tokens"`
	UsageKnownCalls   int    `json:"usage_known_calls"`
	UsageUnknownCalls int    `json:"usage_unknown_calls"`
	UsageInvalidCalls int    `json:"usage_invalid_calls"`
	UsageStatus       string `json:"usage_status"`
	ReportedUsage     *Usage `json:"reported_usage,omitempty"`
	CostStatus        string `json:"cost_status"`
}

type Recorder struct {
	mu             sync.Mutex
	started, ended time.Time
	state          Summary
	calls          []*ModelCall // bounded by MaxModelCalls, shared by WithTools copies
}

type ModelCall struct {
	owner                       *Recorder
	started, ended              time.Time
	reported, invalid, complete bool
	usage                       Usage
}

func Start(ctx context.Context, executionID string) (context.Context, *Recorder) {
	r := &Recorder{started: time.Now(), state: Summary{ExecutionID: executionID, Outcome: "running", CostStatus: "unknown"}}
	return context.WithValue(ctx, contextKey{}, r), r
}

func From(ctx context.Context) *Recorder {
	r, _ := ctx.Value(contextKey{}).(*Recorder)
	return r
}

func (r *Recorder) update(fn func(*Summary)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended.IsZero() {
		fn(&r.state)
	}
}

// Stage accepts only internal finite stage names; nothing user supplied is kept.
func (r *Recorder) Stage(name string) func() {
	start := time.Now()
	return func() {
		r.update(func(s *Summary) {
			duration := time.Since(start).Milliseconds()
			switch name {
			case "lock":
				s.LockWaitMS += duration
			case "generation":
				s.GenerationMS += duration
			case "finalization":
				s.FinalizationMS += duration
			case "save":
				s.SaveMS += duration
			}
		})
	}
}

func (r *Recorder) Replay() { r.update(func(s *Summary) { s.Replayed = true }) }
func (r *Recorder) Commit() { r.update(func(s *Summary) { s.Committed = true }) }

func (r *Recorder) Route(path string, cause error) {
	if path != "primary" && path != "fallback_only" && path != "fallback" {
		return
	}
	r.update(func(s *Summary) {
		s.Path = path
		if cause != nil {
			s.FallbackCode = safety.ErrorCode(cause)
		}
	})
}

// Provider labels the configured retrieval provider, not proof that retrieval
// ran or that candidates came from a particular hybrid channel.
func (r *Recorder) Provider(name string) {
	r.update(func(s *Summary) { s.SelectedProvider = safety.Label(name) })
}

// Sent measures successful RPC Send completion, not browser render or first
// internal model token. Heartbeats/tool events cannot become first delta time.
func (r *Recorder) Sent(kind string, answerBytes int) {
	r.update(func(s *Summary) {
		s.EventsSent++
		if kind == streamcontract.ToolStarted {
			s.ToolStartsSent++
		}
		if kind == streamcontract.AnswerDelta && answerBytes > 0 {
			s.AnswerBytesSent += answerBytes
			if s.FirstDeltaMS == nil {
				elapsed := time.Since(r.started).Milliseconds()
				s.FirstDeltaMS = &elapsed
			}
		}
	})
}

func (r *Recorder) BeginModel(estimatedTokens int64) *ModelCall {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ended.IsZero() || len(r.calls) >= streamcontract.MaxModelCalls {
		return nil
	}
	c := &ModelCall{owner: r, started: time.Now()}
	r.calls = append(r.calls, c)
	r.state.EstimatedTokens += estimatedTokens
	return c
}

func (c *ModelCall) Observe(u Usage) {
	if c == nil {
		return
	}
	r := c.owner
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ended.IsZero() || !c.ended.IsZero() {
		return
	}
	// Finite non-negative counts, internally consistent and monotonic. Repeated
	// snapshots are not summed; provider delta-style usage is not guessed at.
	if u.Prompt < 0 || u.Completion < 0 || u.Total < 0 || u.Total > 1_000_000_000 ||
		u.Prompt > u.Total || u.Completion != u.Total-u.Prompt ||
		c.reported && (u.Prompt < c.usage.Prompt || u.Completion < c.usage.Completion || u.Total < c.usage.Total) {
		c.invalid = true
		return
	}
	c.reported, c.usage = true, u
}

func (c *ModelCall) End(normalEOF bool) {
	if c == nil {
		return
	}
	r := c.owner
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended.IsZero() && c.ended.IsZero() {
		c.ended, c.complete = time.Now(), normalEOF
	}
}

// Finish freezes metadata once. A business error can have a successful gRPC
// transport (error + done(false)), so callers must pass that business cause too.
func (r *Recorder) Finish(cause error, transportFailed bool) Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended.IsZero() {
		r.ended = time.Now()
		r.state.ErrorCode = safety.ErrorCode(cause)
		switch {
		case errors.Is(cause, context.Canceled) || status.Code(cause) == codes.Canceled:
			r.state.Outcome = "canceled"
		case errors.Is(cause, context.DeadlineExceeded) || status.Code(cause) == codes.DeadlineExceeded:
			r.state.Outcome = "deadline_exceeded"
		case transportFailed:
			r.state.Outcome = "transport_error"
		case cause != nil:
			r.state.Outcome = "failed"
		case r.state.Replayed:
			r.state.Outcome = "replayed"
		default:
			r.state.Outcome = "succeeded"
		}
	}
	return r.snapshot()
}

func (r *Recorder) Snapshot() Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot()
}

func (r *Recorder) snapshot() Summary {
	s := r.state
	end := r.ended
	if end.IsZero() {
		end = time.Now()
	}
	s.DurationMS = end.Sub(r.started).Milliseconds()
	if s.FirstDeltaMS != nil {
		v := *s.FirstDeltaMS
		s.FirstDeltaMS = &v
	}
	s.ModelCalls = len(r.calls)
	var usage Usage
	for _, call := range r.calls {
		callEnd := call.ended
		if callEnd.IsZero() {
			callEnd = end
		}
		s.ModelMS += callEnd.Sub(call.started).Milliseconds()
		if call.invalid {
			s.UsageInvalidCalls++
		}
		if call.complete && call.reported && !call.invalid {
			s.UsageKnownCalls++
			usage.Prompt += call.usage.Prompt
			usage.Completion += call.usage.Completion
			usage.Total += call.usage.Total
		} else {
			s.UsageUnknownCalls++
		}
	}
	switch {
	case s.ModelCalls == 0:
		s.UsageStatus = "no_model_calls"
	case s.UsageKnownCalls == 0:
		s.UsageStatus = "unknown"
	case s.UsageUnknownCalls != 0:
		s.UsageStatus = "partial"
	default:
		s.UsageStatus = "complete"
	}
	if s.UsageKnownCalls > 0 {
		s.ReportedUsage = &usage
	}
	return s
}
