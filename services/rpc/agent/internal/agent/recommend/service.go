package recommend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

// Service 编排推荐流程。
//
// 它持有两个实现了 agentcore.Agent 的推荐器：
//   - primary 是首选编排器（通常是 Eino ReAct LLM Agent），未配置模型时为 nil；
//   - fallback 是确定性规则推荐 Agent，始终可用。
//
// primary 可用时优先使用；primary 执行失败则降级到 fallback，并记录失败原因。
// 这样 LLM 链路是真正的编排主入口，规则推荐只承担兜底职责，二者不再各跑一遍。
//
// 会话记忆的写入统一收口在这里：无论结果来自 primary 还是 fallback，
// 成功返回前都原子保存原始请求、结构化状态与完整结果。Agent 实现只读不写；
// 本地锁与存储层锁共同串行化同一用户同一会话，turn_id 用于安全重试。
type Service struct {
	primary  agentcore.Agent
	fallback agentcore.Agent
	memory   memory.Manager
	locks    *conversationLocker
}

// NewService 创建推荐编排服务。fallback 必填，primary 与 mem 可为 nil（nil 记忆表示无多轮能力）。
func NewService(fallback, primary agentcore.Agent, mem memory.Manager) *Service {
	return &Service{
		primary:  primary,
		fallback: fallback,
		memory:   mem,
		locks:    newConversationLocker(),
	}
}

// Recommend 执行推荐流程。
func (s *Service) Recommend(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if s == nil || s.fallback == nil {
		return nil, agentcore.ErrAgentNotFound
	}
	if input.ConversationId == "" {
		input.ConversationId = uuid.NewString()
	}
	if input.TurnId == "" {
		input.TurnId = uuid.NewString()
	}
	release, err := s.locks.acquire(ctx, conversationLockKey{
		userId:         input.UserId,
		conversationId: input.ConversationId,
	})
	if err != nil {
		logx.WithContext(ctx).Errorw("wait for conversation execution failed",
			logx.Field("user_id", input.UserId),
			logx.Field("conversation_id", input.ConversationId),
			logx.Field("error", err.Error()),
		)
		return nil, err
	}
	defer release()

	store, hasStore := s.memory.(memory.ConversationStore)
	if hasStore {
		var result *agentcore.Result
		err = store.WithConversationLock(ctx, input.UserId, input.ConversationId, func(lockedCtx context.Context) error {
			var executeErr error
			result, executeErr = s.recommendWithStore(lockedCtx, store, input)
			return executeErr
		})
		if err != nil {
			logx.WithContext(ctx).Errorw("recommendation failed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId), logx.Field("error", err.Error()))
			return nil, err
		}
		logx.WithContext(ctx).Infow("recommendation completed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId))
		return result, nil
	}

	result, err := s.run(ctx, input)
	if err != nil {
		logx.WithContext(ctx).Errorw("recommendation failed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId), logx.Field("error", err.Error()))
		return nil, err
	}

	result.ConversationId = input.ConversationId
	result.ConversationTitle = s.conversationTitle(ctx, input)
	result.TurnId = input.TurnId
	s.remember(ctx, input, result)
	logx.WithContext(ctx).Infow("recommendation completed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId))
	return result, nil
}

// recommendWithStore 在会话锁内完成幂等检查、状态恢复、Agent 执行和原子保存。
// 这四步不能拆开，否则并发请求可能生成重复轮次或读取过期约束。
func (s *Service) recommendWithStore(ctx context.Context, store memory.ConversationStore, input agentcore.Input) (*agentcore.Result, error) {
	if saved, found, err := store.FindTurn(ctx, input.UserId, input.ConversationId, input.TurnId); err != nil {
		return nil, err
	} else if found {
		return decodeSavedResult(saved)
	}
	if conversation, exists, err := store.GetConversation(ctx, input.UserId, input.ConversationId); err != nil {
		return nil, err
	} else if exists {
		prior := intentFromState(conversation.State)
		input.PriorIntent = &prior
	}
	result, err := s.run(ctx, input)
	if err != nil {
		return nil, err
	}
	result.ConversationId = input.ConversationId
	result.ConversationTitle = s.conversationTitle(ctx, input)
	result.TurnId = input.TurnId
	storedConversation, _, err := s.saveTurn(ctx, store, input, result)
	if err != nil {
		return nil, err
	}
	result.ConversationTitle = storedConversation.Title
	return result, nil
}

// run 按 primary 优先、失败降级的顺序执行推荐。
func (s *Service) run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if s.primary == nil {
		return s.fallback.Run(ctx, input)
	}

	result, err := s.primary.Run(ctx, input)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, agentcore.ErrContextTooLarge) {
		return nil, err
	}
	return s.fallbackAfterFailure(ctx, input, err)
}

// saveTurn 将领域结果序列化后，与当前结构化意图一并原子持久化。
func (s *Service) saveTurn(ctx context.Context, store memory.ConversationStore, input agentcore.Input, result *agentcore.Result) (memory.Conversation, memory.Turn, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return memory.Conversation{}, memory.Turn{}, fmt.Errorf("encode recommendation result: %w", err)
	}
	return store.SaveTurn(ctx, memory.SaveTurnReq{
		UserId: input.UserId, ConversationId: input.ConversationId, TurnId: input.TurnId,
		Title: result.ConversationTitle, Query: input.Query, BudgetCents: input.BudgetCents,
		MaxItems: input.MaxItems, Intent: stateFromIntent(result.Intent),
		ResultJSON: resultJSON, Summary: result.Summary,
	})
}

// decodeSavedResult 恢复幂等轮次结果，并以存储主键覆盖可能过时的 JSON 标识。
func decodeSavedResult(turn memory.Turn) (*agentcore.Result, error) {
	var result agentcore.Result
	if err := json.Unmarshal(turn.ResultJSON, &result); err != nil {
		return nil, fmt.Errorf("decode saved recommendation result: %w", err)
	}
	result.ConversationId = turn.ConversationId
	result.TurnId = turn.TurnId
	return &result, nil
}

// stateFromIntent 提取需要跨轮长期保留的推荐约束。
func stateFromIntent(intent agentcore.Intent) memory.IntentState {
	return memory.IntentState{BudgetCents: intent.BudgetCents, MaxItems: intent.MaxItems,
		Keywords: append([]string(nil), intent.Keywords...), Preferences: append([]string(nil), intent.Preferences...)}
}

// intentFromState 把持久化状态恢复为 Planner 可继承的上一轮意图。
func intentFromState(state memory.IntentState) agentcore.Intent {
	return agentcore.Intent{BudgetCents: state.BudgetCents, MaxItems: state.MaxItems,
		Keywords: append([]string(nil), state.Keywords...), Preferences: append([]string(nil), state.Preferences...)}
}

// ListConversations 返回当前认证用户的会话列表。
func (s *Service) ListConversations(ctx context.Context, userId string, page, pageSize int) ([]memory.Conversation, int64, error) {
	store, ok := s.memory.(memory.ConversationStore)
	if !ok {
		return nil, 0, fmt.Errorf("conversation store is not configured")
	}
	return store.ListConversations(ctx, userId, page, pageSize)
}

// ListTurns 返回会话元数据和按时间正序排列的完整轮次。
func (s *Service) ListTurns(ctx context.Context, userId, conversationId string, page, pageSize int) (memory.Conversation, []memory.Turn, int64, bool, error) {
	store, ok := s.memory.(memory.ConversationStore)
	if !ok {
		return memory.Conversation{}, nil, 0, false, fmt.Errorf("conversation store is not configured")
	}
	return store.ListTurns(ctx, userId, conversationId, page, pageSize)
}

// DeleteConversation 删除当前用户拥有的指定会话及其轮次。
func (s *Service) DeleteConversation(ctx context.Context, userId, conversationId string) (bool, error) {
	store, ok := s.memory.(memory.ConversationStore)
	if !ok {
		return false, fmt.Errorf("conversation store is not configured")
	}
	releaseLocal, err := s.locks.acquire(ctx, conversationLockKey{userId: userId, conversationId: conversationId})
	if err != nil {
		return false, err
	}
	defer releaseLocal()
	var deleted bool
	err = store.WithConversationLock(ctx, userId, conversationId, func(lockedCtx context.Context) error {
		var deleteErr error
		deleted, deleteErr = store.DeleteConversation(lockedCtx, userId, conversationId)
		return deleteErr
	})
	return deleted, err
}

// remember 把本轮问答对写入会话记忆。
//
//   - user 消息存原始 Query：意图脚手架等增强文本只存在于单次模型输入中，入库会逐轮重复污染窗口；
//   - assistant 消息存 Summary：它永远非空（无模型文本时有确定性兜底摘要）、与用户所见一致，
//     且 fallback 路径没有模型原文，统一存 Summary 让两条路径写入逻辑相同。
//
// 仅供不支持 ConversationStore 的兼容实现；正式实现通过 SaveTurn 保存完整轮次。
func (s *Service) remember(ctx context.Context, input agentcore.Input, result *agentcore.Result) {
	if s.memory == nil {
		return
	}
	err := s.memory.Append(ctx, input.UserId, input.ConversationId,
		schema.UserMessage(input.Query),
		schema.AssistantMessage(result.Summary, nil),
	)
	if err != nil {
		logx.WithContext(ctx).Errorw("append conversation memory failed",
			logx.Field("conversation_id", input.ConversationId),
			logx.Field("error", err.Error()),
		)
	}
}

// conversationTitle 以首条用户问题作为稳定标题，并独立于滚动消息窗口持久化。
func (s *Service) conversationTitle(ctx context.Context, input agentcore.Input) string {
	candidate := shortTitle(input.Query)
	if store, ok := s.memory.(memory.ConversationStore); ok {
		conversation, exists, err := store.GetConversation(ctx, input.UserId, input.ConversationId)
		if err != nil {
			logx.WithContext(ctx).Errorw("load conversation title failed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId), logx.Field("error", err.Error()))
		} else if exists && strings.TrimSpace(conversation.Title) != "" {
			return conversation.Title
		}
		return candidate
	}
	if s.memory != nil {
		title, err := s.memory.GetOrCreateTitle(ctx, input.UserId, input.ConversationId, candidate)
		if err != nil {
			logx.WithContext(ctx).Errorw("load or create conversation title failed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId), logx.Field("error", err.Error()))
		} else if strings.TrimSpace(title) != "" {
			return title
		}
	}
	return candidate
}

// shortTitle 按字符截断，避免截断中文等多字节字符。
func shortTitle(text string) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= 32 {
		return text
	}
	return string([]rune(text)[:32]) + "…"
}

// fallbackAfterFailure 在 primary 失败时降级到 fallback，并附加一条失败工具记录。
func (s *Service) fallbackAfterFailure(ctx context.Context, input agentcore.Input, cause error) (*agentcore.Result, error) {
	result, err := s.fallback.Run(ctx, input)
	if err != nil {
		return nil, err
	}
	result.ToolsUsed = append(result.ToolsUsed, agentcore.ToolCall{
		Name:    "primary." + s.primary.Name(),
		Success: false,
		Detail:  cause.Error(),
	})
	return result, nil
}
