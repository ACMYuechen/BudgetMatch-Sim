package agent

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"budgetmatch-sim/cmd/app/internal/types"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func validStreamRequest(req *types.AgentRecommendReq) bool {
	if req == nil || !utf8.ValidString(req.Query) || strings.TrimSpace(req.Query) == "" || utf8.RuneCountInString(req.Query) > 2000 ||
		req.BudgetCents < 0 || req.BudgetCents > 100000000000 || req.MaxItems < 0 || req.MaxItems > 10 {
		return false
	}
	for _, id := range []string{req.ConversationId, req.TurnId} {
		if !utf8.ValidString(id) || utf8.RuneCountInString(id) > 128 || strings.TrimSpace(id) != id {
			return false
		}
	}
	return true
}

// AgentRecommendStream makes exactly one RPC attempt. Terminal frames are held
// until a valid done and normal RPC EOF, so a truncated upstream is not success.
func (l *AgentRecommendStreamLogic) AgentRecommendStream(req *types.AgentRecommendReq, emit func(StreamEvent) error) error {
	if _, err := request.MustUserId(l.ctx); err != nil {
		return err
	}
	if !validStreamRequest(req) || emit == nil {
		return apperrors.Invalid
	}
	if l.svcCtx == nil || l.svcCtx.AgentClient == nil {
		return apperrors.Internal
	}
	ctx, cancel := context.WithTimeout(l.ctx, streamcontract.MaxDuration)
	defer cancel()
	in := &pb.RecommendReq{Query: req.Query, BudgetCents: req.BudgetCents, MaxItems: int32(req.MaxItems), ConversationId: req.ConversationId, TurnId: req.TurnId}
	if in.ConversationId == "" {
		in.ConversationId = uuid.NewString()
	}
	if in.TurnId == "" {
		in.TurnId = uuid.NewString()
	}
	state := newRPCStreamState(in.ConversationId, in.TurnId)
	send := func(event *types.AgentStreamEvent) error {
		return emit(StreamEvent{ID: event.ExecutionId + ":" + strconv.FormatUint(event.Sequence, 10), Event: event.Event, Data: event})
	}
	fail := func(cause error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Ordinary errors are not validator messages and must never be exposed.
		if _, ok := status.FromError(cause); !ok {
			cause = status.Error(codes.Internal, "upstream stream failed")
		}
		public := publicStreamError(cause).(apperrors.HTTPResponse)
		event := state.localEvent(streamcontract.Error)
		event.Error = &types.AgentStreamError{Code: public.Code, Message: public.Message,
			Retryable: status.Code(cause) == codes.Unavailable}
		if err := send(event); err != nil {
			return err
		}
		event = state.localEvent(streamcontract.Done)
		event.Done = &types.AgentStreamDone{Ok: false}
		if err := send(event); err != nil {
			return err
		}
		return cause
	}
	source, err := l.svcCtx.AgentClient.RecommendStream(ctx, in, grpc.MaxCallRecvMsgSize(streamcontract.MaxEventBytes))
	if err != nil {
		return fail(err)
	}
	if source == nil {
		return fail(streamProtocolError())
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := source.Recv()
		if stopped := ctx.Err(); stopped != nil {
			return stopped
		}
		if errors.Is(err, io.EOF) {
			if state.done == nil {
				return fail(streamProtocolError())
			}
			if err := send(state.outcome); err != nil {
				return err
			}
			return send(state.done)
		}
		if err != nil {
			return fail(err)
		}
		event, err := state.accept(frame)
		if err != nil {
			return fail(err)
		}
		if event != nil {
			if err := send(event); err != nil {
				return err
			}
			state.sent = event.Sequence
		}
	}
}

func streamProtocolError() error {
	return status.Error(codes.FailedPrecondition, "recommendation stream protocol mismatch")
}
