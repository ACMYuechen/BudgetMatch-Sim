package recommend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/safety"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ExecuteDemand executes exactly the latest ready planning revision. It takes
// no free text, patch or numeric override: changing demand requires PlanDemand.
// Completed retries replay BEFORE checking the current plan or enabled mode.
func (s *Service) ExecuteDemand(ctx context.Context, userID, conversationID, planTurnID, turnID string) (*agentcore.Result, error) {
	if s == nil {
		return nil, agentcore.ErrAgentNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if userID == "" {
		return nil, agentcore.ErrInvalidInput
	}
	for field, value := range map[string]string{"conversation_id": conversationID, "plan_turn_id": planTurnID} {
		if err := validateRequiredID(field, value); err != nil {
			return nil, err
		}
	}
	if err := validateOptionalID("turn_id", turnID); err != nil {
		return nil, err
	}
	if turnID == "" {
		turnID = uuid.NewString()
	}
	store, ok := s.memory.(memory.ConversationStore)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "demand execution requires a conversation store")
	}
	// Namespace prevents a plan/recommend/execute turn ID from being reused
	// across methods even when the visible text and numeric inputs coincide.
	sum := sha256.Sum256([]byte("execute-demand-v1:" + planTurnID))
	fingerprint := hex.EncodeToString(sum[:])
	input := agentcore.Input{UserId: userID, ConversationId: conversationID, TurnId: turnID, Query: "执行已确认的需求规划"}
	release, err := s.locks.acquire(ctx, conversationLockKey{userId: userID, conversationId: conversationID})
	if err != nil {
		return nil, err
	}
	defer release()
	var result *agentcore.Result
	err = store.WithConversationLock(ctx, userID, conversationID, func(lockedCtx context.Context) error {
		var executeErr error
		result, executeErr = s.executeDemandWithStore(lockedCtx, store, input, planTurnID, fingerprint)
		return executeErr
	})
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err != nil {
		return nil, safety.Protect(err)
	}
	return result, nil
}

func (s *Service) executeDemandWithStore(ctx context.Context, store memory.ConversationStore, input agentcore.Input, planTurnID, fingerprint string) (*agentcore.Result, error) {
	if saved, found, err := store.FindTurn(ctx, input.UserId, input.ConversationId, input.TurnId); err != nil {
		return nil, err
	} else if found {
		if !sameTurnRequest(saved, input, fingerprint) {
			return nil, agentcore.ErrTurnConflict
		}
		return decodeSavedResult(saved)
	}
	conversation, exists, err := store.GetConversation(ctx, input.UserId, input.ConversationId)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, status.Error(codes.NotFound, "conversation not found")
	}
	state := conversation.State
	if !state.PlanningOnly || !state.DemandPlanReady || state.Demand == nil || state.DemandPlanTurnID != planTurnID {
		return nil, status.Error(codes.FailedPrecondition, "current demand plan is not ready or has changed")
	}
	if s.demandExecutor == nil {
		return nil, agentcore.ErrDemandNotExecutable
	}
	result, err := s.demandExecutor.Run(ctx, intentFromState(state))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.Execution.PlanTurnID = planTurnID
	result.ConversationId, result.ConversationTitle, result.TurnId = input.ConversationId, conversation.Title, input.TurnId
	result.ToolsUsed = safety.ToolCalls(result.ToolsUsed)
	encoded, err := marshalTurnResult(result, fingerprint)
	if err != nil {
		return nil, agentcore.ErrUnsafeResult
	}
	// Preserve the exact active state and planning revision, including the
	// legacy-method guard. Execution cannot clear or rewrite accepted demand.
	_, _, err = store.SaveTurn(ctx, memory.SaveTurnReq{UserId: input.UserId, ConversationId: input.ConversationId,
		TurnId: input.TurnId, Title: conversation.Title, Query: input.Query, Intent: state, ResultJSON: encoded, Summary: result.Summary})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
