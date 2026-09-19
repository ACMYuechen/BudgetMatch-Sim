package recommendservicelogic

import (
	"context"
	"errors"
	"regexp"
	"unicode/utf8"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

var progressCallID = regexp.MustCompile(`^tool-[1-9][0-9]{0,2}$`)
var progressErrorCode = regexp.MustCompile(`^(canceled|deadline_exceeded|permission_denied|not_found|already_exists|output_limit|invalid_argument|context_limit|unsafe_result|turn_conflict|execution_failed|rpc_(canceled|deadlineexceeded|permissiondenied|unauthenticated|unavailable|internal|unknown|invalidargument|notfound|alreadyexists|resourceexhausted|failedprecondition|aborted|outofrange|unimplemented|dataloss))$`)

// streamProgress is a zero-queue, single-flight sink. Producers wait with their
// context; only one Send runs at a time. The transport deadline bounds Send.
// Close is a barrier before final/error, so no late progress can follow them.
type streamProgress struct {
	gate                       chan struct{}
	send                       func(*pb.RecommendStreamEvent) error
	closed                     bool
	failure                    error
	events, bytes, answerBytes int
	calls                      map[string]progressCall
}

type progressCall struct {
	name      string
	completed bool
}

func newStreamProgress(send func(*pb.RecommendStreamEvent) error) *streamProgress {
	return &streamProgress{gate: make(chan struct{}, 1), send: send, calls: make(map[string]progressCall)}
}

func (p *streamProgress) Close() {
	p.gate <- struct{}{}
	p.closed = true
	<-p.gate
}

func (p *streamProgress) Err() error {
	p.gate <- struct{}{}
	defer func() { <-p.gate }()
	return p.failure
}

func (p *streamProgress) Emit(ctx context.Context, event agentcore.Progress) (err error) {
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() {
		if err != nil && p.failure == nil {
			p.failure = err
		}
		<-p.gate
	}()
	if p.failure != nil {
		return p.failure
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.closed {
		return agentcore.ErrStreamInterrupted
	}
	frame, err := p.frame(event)
	if err != nil {
		return errors.Join(agentcore.ErrStreamInterrupted, err)
	}
	// Counts progress payload only, before the common envelope is stamped.
	size := proto.Size(frame)
	if p.events >= streamcontract.MaxProgressEvents || size > streamcontract.MaxProgressBytes-p.bytes {
		return progressLimitError()
	}
	p.events++
	p.bytes += size
	if err := p.send(frame); err != nil {
		return errors.Join(agentcore.ErrStreamInterrupted, err)
	}
	return nil
}

func (p *streamProgress) frame(event agentcore.Progress) (*pb.RecommendStreamEvent, error) {
	frame := &pb.RecommendStreamEvent{Event: event.Kind}
	if event.Kind == streamcontract.AnswerDelta {
		if event.Text == "" || !utf8.ValidString(event.Text) || event.CallID != "" || event.ToolName != "" || event.Status != "" || event.ErrorCode != "" || event.DurationMS != 0 {
			return nil, progressProtocolError()
		}
		if len(event.Text) > streamcontract.MaxDeltaBytes || len(event.Text) > streamcontract.MaxAnswerBytes-p.answerBytes {
			return nil, progressLimitError()
		}
		p.answerBytes += len(event.Text)
		frame.Payload = &pb.RecommendStreamEvent_AnswerDelta{AnswerDelta: &pb.StreamAnswerDelta{Text: event.Text, Provisional: true}}
		return frame, nil
	}
	if event.Kind != streamcontract.ToolStarted && event.Kind != streamcontract.ToolCompleted ||
		!progressCallID.MatchString(event.CallID) || event.ToolName == "" || event.Text != "" || event.DurationMS < 0 || event.DurationMS > streamcontract.MaxDuration.Milliseconds() {
		return nil, progressProtocolError()
	}
	name := safety.ToolCalls([]agentcore.ToolCall{{Name: event.ToolName}})[0].Name
	call, exists := p.calls[event.CallID]
	if event.Kind == streamcontract.ToolStarted {
		if exists || event.Status != "running" || event.ErrorCode != "" || event.DurationMS != 0 {
			return nil, progressProtocolError()
		}
		if len(p.calls) >= streamcontract.MaxToolCalls {
			return nil, progressLimitError()
		}
		p.calls[event.CallID] = progressCall{name: name}
	} else {
		if !exists || call.completed || name != call.name || event.Status != "succeeded" && event.Status != "failed" {
			return nil, progressProtocolError()
		}
		call.completed = true
		p.calls[event.CallID] = call
		if event.Status == "succeeded" {
			event.ErrorCode = ""
		} else if !progressErrorCode.MatchString(event.ErrorCode) {
			event.ErrorCode = "execution_failed"
		}
	}
	frame.Payload = &pb.RecommendStreamEvent_Tool{Tool: &pb.StreamToolEvent{CallId: event.CallID, Name: name,
		Status: event.Status, DurationMs: event.DurationMS, ErrorCode: event.ErrorCode}}
	return frame, nil
}

func progressLimitError() error {
	return errors.Join(agentcore.ErrStreamInterrupted, agentcore.ErrStreamLimit, status.Error(codes.ResourceExhausted, "agent stream resource limit"))
}

func progressProtocolError() error {
	return status.Error(codes.FailedPrecondition, "agent progress protocol mismatch")
}
