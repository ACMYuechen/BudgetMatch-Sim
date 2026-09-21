package agent

import (
	"regexp"
	"unicode/utf8"

	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

var streamExecutionID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var streamEventName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var streamToolID = regexp.MustCompile(`^tool-[1-9][0-9]{0,2}$`)
var streamToolName = regexp.MustCompile(`^tool\.(search_products|select_bundle|read_file|write_file|external_[0-9a-f]{16})$`)
var streamToolError = regexp.MustCompile(`^(canceled|deadline_exceeded|permission_denied|not_found|already_exists|output_limit|invalid_argument|context_limit|unsafe_result|turn_conflict|execution_failed|rpc_(canceled|deadlineexceeded|permissiondenied|unauthenticated|unavailable|internal|unknown|invalidargument|notfound|alreadyexists|resourceexhausted|failedprecondition|aborted|outofrange|unimplemented|dataloss))$`)

// Only the bounded terminal pair is buffered. Progress is sent synchronously.
type rpcStreamState struct {
	conversation, turn, execution string
	received, sent                uint64
	accepted                      bool
	progress, bytes, answerBytes  int
	calls                         map[string]types.AgentStreamTool
	outcome, done                 *types.AgentStreamEvent
}

func newRPCStreamState(conversation, turn string) *rpcStreamState {
	return &rpcStreamState{conversation: conversation, turn: turn, calls: make(map[string]types.AgentStreamTool)}
}

func (s *rpcStreamState) localEvent(kind string) *types.AgentStreamEvent {
	if s.execution == "" {
		s.execution = uuid.NewString()
	}
	s.sent++
	return &types.AgentStreamEvent{SchemaVersion: streamcontract.Version, ExecutionId: s.execution,
		ConversationId: s.conversation, TurnId: s.turn, Sequence: s.sent, Event: kind}
}

func (s *rpcStreamState) accept(frame *pb.RecommendStreamEvent) (*types.AgentStreamEvent, error) {
	if frame == nil || frame.SchemaVersion != streamcontract.Version || !streamExecutionID.MatchString(frame.ExecutionId) ||
		frame.ConversationId != s.conversation || frame.TurnId != s.turn || frame.Sequence != s.received+1 ||
		s.execution != "" && frame.ExecutionId != s.execution || !streamEventName.MatchString(frame.Event) || s.done != nil {
		return nil, streamProtocolError()
	}
	size := proto.Size(frame)
	if size > streamcontract.MaxEventBytes || size > streamcontract.MaxHTTPBytes-s.bytes || s.received >= streamcontract.MaxProgressEvents+3 {
		return nil, streamProtocolError()
	}
	s.bytes += size
	s.execution = frame.ExecutionId
	event := &types.AgentStreamEvent{SchemaVersion: frame.SchemaVersion, ExecutionId: frame.ExecutionId,
		ConversationId: frame.ConversationId, TurnId: frame.TurnId, Sequence: frame.Sequence, Event: frame.Event}
	switch frame.Event {
	case streamcontract.Accepted:
		if s.accepted || s.outcome != nil || frame.GetAccepted() == nil {
			return nil, streamProtocolError()
		}
		s.accepted = true
		event.Accepted = &types.AgentStreamAccepted{}
	case streamcontract.AnswerDelta:
		delta := frame.GetAnswerDelta()
		if !s.accepted || s.outcome != nil || delta == nil || !delta.Provisional || delta.Text == "" || !utf8.ValidString(delta.Text) ||
			len(delta.Text) > streamcontract.MaxDeltaBytes || len(delta.Text) > streamcontract.MaxAnswerBytes-s.answerBytes {
			return nil, streamProtocolError()
		}
		s.answerBytes += len(delta.Text)
		s.progress++
		event.AnswerDelta = &types.AgentStreamAnswerDelta{Text: delta.Text, Provisional: true}
	case streamcontract.ToolStarted, streamcontract.ToolCompleted:
		tool := frame.GetTool()
		if !s.accepted || s.outcome != nil || tool == nil || !streamToolID.MatchString(tool.CallId) || !streamToolName.MatchString(tool.Name) ||
			tool.DurationMs < 0 || tool.DurationMs > streamcontract.MaxDuration.Milliseconds() {
			return nil, streamProtocolError()
		}
		prior, found := s.calls[tool.CallId]
		if frame.Event == streamcontract.ToolStarted {
			if found || len(s.calls) >= streamcontract.MaxToolCalls || tool.Status != "running" || tool.DurationMs != 0 || tool.ErrorCode != "" {
				return nil, streamProtocolError()
			}
		} else if !found || prior.Status != "running" || prior.Name != tool.Name ||
			(tool.Status != "succeeded" && tool.Status != "failed") ||
			(tool.Status == "failed" && !streamToolError.MatchString(tool.ErrorCode)) || tool.Status == "succeeded" && tool.ErrorCode != "" {
			return nil, streamProtocolError()
		}
		event.Tool = &types.AgentStreamTool{CallId: tool.CallId, Name: tool.Name, Status: tool.Status, DurationMs: tool.DurationMs, ErrorCode: tool.ErrorCode}
		s.calls[tool.CallId] = *event.Tool
		s.progress++
	case streamcontract.Final:
		final := frame.GetFinal()
		if s.outcome != nil || final == nil || final.Intent == nil || final.ConversationId != s.conversation || final.TurnId != s.turn || len(final.Items) > 10 {
			return nil, streamProtocolError()
		}
		for _, call := range s.calls {
			if call.Status == "running" {
				return nil, streamProtocolError()
			}
		}
		event.Final = mapRecommendResp(final)
		s.outcome = event
	case streamcontract.Error:
		failure := frame.GetError()
		if s.outcome != nil || failure == nil || failure.Code < 400000 || failure.Code > 599999 || failure.Message == "" ||
			len(failure.Message) > 1024 || !utf8.ValidString(failure.Message) {
			return nil, streamProtocolError()
		}
		event.Error = &types.AgentStreamError{Code: failure.Code, Message: failure.Message, Retryable: failure.Retryable}
		s.outcome = event
	case streamcontract.Done:
		done := frame.GetDone()
		if s.outcome == nil || done == nil || done.Ok != (s.outcome.Final != nil) ||
			done.Ok && done.Replayed != !s.accepted || !done.Ok && done.Replayed {
			return nil, streamProtocolError()
		}
		event.Done = &types.AgentStreamDone{Ok: done.Ok, Replayed: done.Replayed}
		s.done = event
	default:
		// Preserve sequence for forward compatibility, without forwarding any
		// unknown protobuf payload (which has not crossed a public mapper).
		if s.outcome != nil {
			return nil, streamProtocolError()
		}
		s.progress++
	}
	if s.progress > streamcontract.MaxProgressEvents {
		return nil, streamProtocolError()
	}
	s.received = frame.Sequence
	if event == s.outcome || event == s.done {
		return nil, nil
	}
	return event, nil
}
