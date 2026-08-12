package recommendservicelogic

import (
	"context"
	"encoding/json"
	"errors"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/pb"
)

// authenticatedUserId 只信任认证拦截器写入的私有 context key。
// RPC 请求体不接收 user_id，从入口阻止客户端伪造其他用户身份。
func authenticatedUserId(ctx context.Context) (string, error) {
	userId, ok := ctx.Value(interceptor.ContextKeyUserId).(string)
	if !ok || userId == "" {
		return "", apperrors.Unauthorized
	}
	return userId, nil
}

// toPBIntentState 将存储层结构化约束转换为公开 RPC 意图类型。
func toPBIntentState(state memory.IntentState) *pb.Intent {
	return &pb.Intent{BudgetCents: state.BudgetCents, MaxItems: state.MaxItems,
		Keywords: state.Keywords, Preferences: state.Preferences}
}

// toPBConversation 将领域会话转换为不暴露内部 user_id 和 version 的摘要。
func toPBConversation(conversation memory.Conversation) *pb.ConversationSummary {
	return &pb.ConversationSummary{ConversationId: conversation.ConversationId, ConversationTitle: conversation.Title,
		State: toPBIntentState(conversation.State), TurnCount: conversation.TurnCount,
		CreatedAtMs: conversation.CreatedAt.UnixMilli(), UpdatedAtMs: conversation.UpdatedAt.UnixMilli()}
}

// toPBTurn 解码持久化的领域结果并组装完整 RPC 轮次。
func toPBTurn(turn memory.Turn) (*pb.ConversationTurn, error) {
	var result agentcore.Result
	if err := json.Unmarshal(turn.ResultJSON, &result); err != nil {
		return nil, err
	}
	return &pb.ConversationTurn{TurnId: turn.TurnId, Sequence: turn.Sequence, Query: turn.Query,
		BudgetCents: turn.BudgetCents, MaxItems: turn.MaxItems, Intent: toPBIntentState(turn.Intent),
		Result: toPB(&result), CreatedAtMs: turn.CreatedAt.UnixMilli(), CompletedAtMs: turn.CompletedAt.UnixMilli()}, nil
}

// mapRecommendError 将领域错误收敛为网关可本地化的统一业务错误。
func mapRecommendError(err error) error {
	if errors.Is(err, agentcore.ErrContextTooLarge) {
		return apperrors.AgentContextTooLarge
	}
	return err
}

// normalizePBPage 与存储层使用相同的页码规则，并保留接口各自的默认页容量。
func normalizePBPage(page, pageSize int32, defaultSize int32) (int32, int32) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultSize
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
