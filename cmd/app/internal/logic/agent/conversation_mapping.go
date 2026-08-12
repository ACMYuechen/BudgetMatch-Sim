package agent

import (
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"
)

// mapIntent 将 protobuf 意图映射为 HTTP API 类型；protobuf getter 可安全处理 nil。
func mapIntent(intent *recommendservice.Intent) types.AgentIntent {
	return types.AgentIntent{BudgetCents: intent.GetBudgetCents(), MaxItems: intent.GetMaxItems(),
		Keywords: intent.GetKeywords(), Preferences: intent.GetPreferences()}
}

// mapConversationSummary 转换会话元数据及其最新结构化约束。
func mapConversationSummary(item *recommendservice.ConversationSummary) types.AgentConversationSummary {
	return types.AgentConversationSummary{ConversationId: item.GetConversationId(), ConversationTitle: item.GetConversationTitle(),
		State: mapIntent(item.GetState()), TurnCount: item.GetTurnCount(),
		CreatedAtMs: item.GetCreatedAtMs(), UpdatedAtMs: item.GetUpdatedAtMs()}
}

// mapConversationTurn 转换一轮原始请求和保存时的完整推荐结果。
func mapConversationTurn(item *recommendservice.ConversationTurn) types.AgentConversationTurn {
	return types.AgentConversationTurn{TurnId: item.GetTurnId(), Sequence: item.GetSequence(), Query: item.GetQuery(),
		BudgetCents: item.GetBudgetCents(), MaxItems: item.GetMaxItems(), Intent: mapIntent(item.GetIntent()),
		Result: *mapRecommendResp(item.GetResult()), CreatedAtMs: item.GetCreatedAtMs(), CompletedAtMs: item.GetCompletedAtMs()}
}
