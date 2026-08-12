package memory

import (
	"context"
	"encoding/json"
	"time"
)

// TurnStatusCompleted 表示一轮推荐已经生成结果并完成持久化。
const TurnStatusCompleted = "completed"

// IntentState 是跨轮次持久化的结构化推荐约束。
// 它独立于滚动文本窗口，即使早期消息被短期缓存淘汰也能继续继承。
type IntentState struct {
	BudgetCents int64    `json:"budget_cents"`
	MaxItems    int32    `json:"max_items"`
	Keywords    []string `json:"keywords"`
	Preferences []string `json:"preferences"`
}

// Conversation 是一个用户可恢复、可列出和可删除的长期会话。
type Conversation struct {
	UserId         string      `json:"user_id"`
	ConversationId string      `json:"conversation_id"`
	Title          string      `json:"title"`
	State          IntentState `json:"state"`
	Version        int64       `json:"version"`
	TurnCount      int64       `json:"turn_count"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// Turn 保存一轮原始请求、解析意图与完整推荐结果。
// ResultJSON 使用领域结果的 JSON 表示，避免存储层依赖具体 RPC 协议。
type Turn struct {
	UserId         string          `json:"user_id"`
	ConversationId string          `json:"conversation_id"`
	TurnId         string          `json:"turn_id"`
	Sequence       int64           `json:"sequence"`
	Status         string          `json:"status"`
	Query          string          `json:"query"`
	BudgetCents    int64           `json:"budget_cents"`
	MaxItems       int32           `json:"max_items"`
	Intent         IntentState     `json:"intent"`
	ResultJSON     json.RawMessage `json:"result"`
	Summary        string          `json:"summary"`
	CreatedAt      time.Time       `json:"created_at"`
	CompletedAt    time.Time       `json:"completed_at"`
}

// SaveTurnReq 是完成一轮推荐后的原子持久化参数。
type SaveTurnReq struct {
	UserId         string
	ConversationId string
	TurnId         string
	Title          string
	Query          string
	BudgetCents    int64
	MaxItems       int32
	Intent         IntentState
	ResultJSON     json.RawMessage
	Summary        string
	Now            time.Time
}

// ConversationStore 在基础消息记忆之上提供完整会话/轮次能力。
// WithConversationLock 必须在多实例部署中串行化同一用户同一会话的执行；
// SaveTurn 必须同时写入轮次、结构化状态和会话版本，并按 turn_id 保证幂等。
type ConversationStore interface {
	Manager
	WithConversationLock(ctx context.Context, userId, conversationId string, fn func(context.Context) error) error
	GetConversation(ctx context.Context, userId, conversationId string) (Conversation, bool, error)
	FindTurn(ctx context.Context, userId, conversationId, turnId string) (Turn, bool, error)
	SaveTurn(ctx context.Context, req SaveTurnReq) (Conversation, Turn, error)
	ListConversations(ctx context.Context, userId string, page, pageSize int) ([]Conversation, int64, error)
	ListTurns(ctx context.Context, userId, conversationId string, page, pageSize int) (Conversation, []Turn, int64, bool, error)
	DeleteConversation(ctx context.Context, userId, conversationId string) (bool, error)
}

// normalizePage 为所有存储实现提供一致的分页默认值和最大页容量。
func normalizePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// cloneConversation 深拷贝切片字段，防止调用方修改存储中的结构化状态。
func cloneConversation(conversation Conversation) Conversation {
	conversation.State.Keywords = append([]string(nil), conversation.State.Keywords...)
	conversation.State.Preferences = append([]string(nil), conversation.State.Preferences...)
	return conversation
}

// cloneTurn 深拷贝意图切片和 JSON 结果，保持存储实现的只读返回语义。
func cloneTurn(turn Turn) Turn {
	turn.Intent.Keywords = append([]string(nil), turn.Intent.Keywords...)
	turn.Intent.Preferences = append([]string(nil), turn.Intent.Preferences...)
	turn.ResultJSON = append(json.RawMessage(nil), turn.ResultJSON...)
	return turn
}
