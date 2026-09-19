package recommendservicelogic

import (
	"context"
	"time"

	apperrors "budgetmatch-sim/infra/errors"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RecommendStreamLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRecommendStreamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecommendStreamLogic {
	return &RecommendStreamLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RecommendStream emits lifecycle events, never synthesized model deltas. All
// sends are synchronous: no unbounded queue or background send goroutine. The
// admission deadline bounds a stalled transport; Send failure stops execution.
func (l *RecommendStreamLogic) RecommendStream(in *pb.RecommendReq, stream pb.RecommendService_RecommendStreamServer) error {
	if in == nil || stream == nil {
		return apperrors.Invalid
	}
	// Always use the authenticated transport context, not a separate constructor
	// context that could lose cancellation or bypass stream authentication.
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	userID, err := authenticatedUserId(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > streamcontract.MaxDuration {
		return status.Error(codes.InvalidArgument, "stream requires a bounded transport deadline")
	}
	if l.svcCtx == nil || l.svcCtx.RecommendService == nil {
		return apperrors.Internal
	}

	executionID := uuid.NewString()
	var conversationID, turnID string
	var sequence uint64
	var sendErr error
	accepted := false
	send := func(event *pb.RecommendStreamEvent) error {
		if err := ctx.Err(); err != nil {
			sendErr = err
			return err
		}
		sequence++
		event.SchemaVersion, event.ExecutionId, event.Sequence = streamcontract.Version, executionID, sequence
		event.ConversationId, event.TurnId = conversationID, turnID
		sendErr = stream.Send(event)
		if sendErr != nil {
			cancel()
		}
		return sendErr
	}
	result, err := l.svcCtx.RecommendService.RecommendStream(ctx, agentcore.Input{
		Query: in.Query, BudgetCents: in.BudgetCents, MaxItems: in.MaxItems,
		UserId: userID, ConversationId: in.ConversationId, TurnId: in.TurnId,
	}, func(_ context.Context, conversation, turn string) error {
		conversationID, turnID = conversation, turn
		if err := send(&pb.RecommendStreamEvent{Event: streamcontract.Accepted,
			Payload: &pb.RecommendStreamEvent_Accepted{Accepted: &pb.StreamAccepted{}}}); err != nil {
			return err
		}
		accepted = true
		return nil
	})
	// Broken/canceled transports cannot reliably receive an error/done pair.
	// Do not retry Send or use a detached context to finish uncommitted work.
	if sendErr != nil {
		return safety.Protect(sendErr)
	}
	if stopped := ctx.Err(); stopped != nil {
		return status.FromContextError(stopped).Err()
	}
	if err != nil {
		err = mapRecommendError(err)
		logx.WithContext(ctx).Errorw("recommendation stream failed", logx.Field("error_code", safety.ErrorCode(err)))
		if !accepted {
			return err // admission failure: no stream events, sanitized gRPC status
		}
		if err := send(&pb.RecommendStreamEvent{Event: streamcontract.Error,
			Payload: &pb.RecommendStreamEvent_Error{Error: publicRecommendStreamError(err)}}); err != nil {
			return safety.Protect(err)
		}
		return safety.Protect(send(&pb.RecommendStreamEvent{Event: streamcontract.Done,
			Payload: &pb.RecommendStreamEvent_Done{Done: &pb.StreamDone{Ok: false}}}))
	}

	// Service returns only after successful atomic save (or completed replay).
	// A post-commit disconnect does not undo the turn: retry the same IDs to get
	// this historical snapshot without rerunning tools, finalizer or persistence.
	conversationID, turnID = result.ConversationId, result.TurnId
	if err := send(&pb.RecommendStreamEvent{Event: streamcontract.Final,
		Payload: &pb.RecommendStreamEvent_Final{Final: toPB(result)}}); err != nil {
		return safety.Protect(err)
	}
	return safety.Protect(send(&pb.RecommendStreamEvent{Event: streamcontract.Done,
		Payload: &pb.RecommendStreamEvent_Done{Done: &pb.StreamDone{Ok: true, Replayed: !accepted}}}))
}

func publicRecommendStreamError(err error) *pb.StreamError {
	// err has already crossed mapRecommendError; HTTPErrorHandler sees only
	// known AppErrors or protected statuses, never a raw ordinary error string.
	_, body := apperrors.HTTPErrorHandler(err)
	response := body.(apperrors.HTTPResponse)
	code := status.Code(err)
	return &pb.StreamError{Code: response.Code, Message: response.Message,
		Retryable: code == codes.Unavailable || code == codes.ResourceExhausted}
}
