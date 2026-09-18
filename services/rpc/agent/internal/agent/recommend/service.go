package recommend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/safety"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxQueryRunes   = agentcore.MaxQueryRunes
	maxIDRunes      = 128
	maxRequestItems = agentcore.MaxItems
	// maxBudgetCents 为显式预算提供宽松但有限的传输边界，避免异常大整数进入检索与提示词。
	maxBudgetCents int64 = agentcore.MaxBudgetCents
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
	primary   agentcore.Agent
	fallback  agentcore.Agent
	memory    memory.Manager
	locks     *conversationLocker
	finalizer ResultFinalizer
}

// WithFinalizer configures the return/persistence policy during construction.
// Production always installs strict Mall or explicit demo policy. Offline engine
// benchmarks can omit it to retain their fixed-snapshot evaluation protocol.
func (s *Service) WithFinalizer(finalizer ResultFinalizer) *Service {
	s.finalizer = finalizer
	return s
}

// NewService 创建编排服务。Recommend 要求 fallback；PlanDemand 要求完整会话存储。
// primary 与 mem 可为 nil（nil 记忆表示无多轮能力）。
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
	return s.execute(ctx, input, nil, "")
}

// PlanDemand persists a planning-only turn. A separate RPC prevents old servers
// from silently ignoring a new hard constraint on the legacy Recommend method.
func (s *Service) PlanDemand(ctx context.Context, input agentcore.Input, rawPatch string) (*agentcore.Result, error) {
	patch, fingerprint, err := decodeDemandRequest(rawPatch)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, input, &patch, fingerprint)
}

func (s *Service) execute(ctx context.Context, input agentcore.Input, patch *demand.Patch, fingerprint string) (*agentcore.Result, error) {
	if s == nil || patch == nil && s.fallback == nil {
		return nil, agentcore.ErrAgentNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateInput(input); err != nil {
		return nil, err
	}
	store, hasStore := s.memory.(memory.ConversationStore)
	if patch != nil && !hasStore {
		return nil, status.Error(codes.FailedPrecondition, "demand planning requires a conversation store")
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
			logx.Field("user_id", safety.Label(input.UserId)),
			logx.Field("conversation_id", safety.Label(input.ConversationId)),
			logx.Field("error_code", safety.ErrorCode(err)),
		)
		return nil, err
	}
	defer release()

	if hasStore {
		operation := "recommendation"
		if patch != nil {
			operation = "demand planning"
		}
		var result *agentcore.Result
		err = store.WithConversationLock(ctx, input.UserId, input.ConversationId, func(lockedCtx context.Context) error {
			var executeErr error
			result, executeErr = s.recommendWithStore(lockedCtx, store, input, patch, fingerprint)
			return executeErr
		})
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		if err != nil {
			logx.WithContext(ctx).Errorw(operation+" failed", logx.Field("user_id", safety.Label(input.UserId)), logx.Field("conversation_id", safety.Label(input.ConversationId)), logx.Field("error_code", safety.ErrorCode(err)))
			return nil, err
		}
		logx.WithContext(ctx).Infow(operation+" completed", logx.Field("user_id", safety.Label(input.UserId)), logx.Field("conversation_id", safety.Label(input.ConversationId)))
		return result, nil
	}

	result, err := s.run(ctx, input)
	if err != nil {
		logx.WithContext(ctx).Errorw("recommendation failed", logx.Field("user_id", safety.Label(input.UserId)), logx.Field("conversation_id", safety.Label(input.ConversationId)), logx.Field("error_code", safety.ErrorCode(err)))
		return nil, err
	}

	result.ConversationId = input.ConversationId
	result.ConversationTitle = s.conversationTitle(ctx, input)
	result.TurnId = input.TurnId
	s.remember(ctx, input, result)
	logx.WithContext(ctx).Infow("recommendation completed", logx.Field("user_id", safety.Label(input.UserId)), logx.Field("conversation_id", safety.Label(input.ConversationId)))
	return result, nil
}

// validateInput 在业务服务入口兜底校验，使直接 RPC 或内部调用无法绕过 HTTP 参数规则。
func validateInput(input agentcore.Input) error {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return fmt.Errorf("%w: query is blank", agentcore.ErrInvalidInput)
	}
	if utf8.RuneCountInString(input.Query) > maxQueryRunes {
		return fmt.Errorf("%w: query exceeds %d characters", agentcore.ErrInvalidInput, maxQueryRunes)
	}
	if input.BudgetCents < 0 || input.BudgetCents > maxBudgetCents {
		return fmt.Errorf("%w: budget_cents is outside 0..%d", agentcore.ErrInvalidInput, maxBudgetCents)
	}
	if input.MaxItems < 0 || input.MaxItems > maxRequestItems {
		return fmt.Errorf("%w: max_items is outside 0..%d", agentcore.ErrInvalidInput, maxRequestItems)
	}
	if err := validateOptionalID("conversation_id", input.ConversationId); err != nil {
		return err
	}
	if err := validateOptionalID("turn_id", input.TurnId); err != nil {
		return err
	}
	return nil
}

// validateOptionalID 校验客户端可选标识；空字符串表示由服务端生成。
func validateOptionalID(field, value string) error {
	if value == "" {
		return nil
	}
	return validateRequiredID(field, value)
}

// validateRequiredID 拒绝空白、首尾空格和超长标识，避免生成不可稳定寻址的存储键。
func validateRequiredID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%w: %s is blank", agentcore.ErrInvalidInput, field)
	}
	if trimmed != value {
		return fmt.Errorf("%w: %s contains surrounding whitespace", agentcore.ErrInvalidInput, field)
	}
	if utf8.RuneCountInString(value) > maxIDRunes {
		return fmt.Errorf("%w: %s exceeds %d characters", agentcore.ErrInvalidInput, field, maxIDRunes)
	}
	return nil
}

// recommendWithStore 在会话锁内完成幂等检查、状态恢复、Agent 执行和原子保存。
// 这四步不能拆开，否则并发请求可能生成重复轮次或读取过期约束。
func (s *Service) recommendWithStore(ctx context.Context, store memory.ConversationStore, input agentcore.Input, patch *demand.Patch, fingerprint string) (*agentcore.Result, error) {
	if saved, found, err := store.FindTurn(ctx, input.UserId, input.ConversationId, input.TurnId); err != nil {
		return nil, err
	} else if found {
		if !sameTurnRequest(saved, input, fingerprint) {
			return nil, agentcore.ErrTurnConflict
		}
		return decodeSavedResult(saved)
	}
	if conversation, exists, err := store.GetConversation(ctx, input.UserId, input.ConversationId); err != nil {
		return nil, err
	} else if exists {
		if patch == nil && conversation.State.PlanningOnly {
			return nil, agentcore.ErrDemandNotExecutable
		}
		prior := intentFromState(conversation.State)
		input.PriorIntent = &prior
	}
	var result *agentcore.Result
	var err error
	if patch != nil {
		result, err = s.planDemand(ctx, input, *patch)
	} else {
		result, err = s.run(ctx, input)
	}
	if err != nil {
		return nil, err
	}
	result.ConversationId = input.ConversationId
	result.ConversationTitle = s.conversationTitle(ctx, input)
	result.TurnId = input.TurnId
	storedConversation, _, err := s.saveTurn(ctx, store, input, result, fingerprint)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.ConversationTitle = storedConversation.Title
	return result, nil
}

// sameTurnRequest 确保幂等重放只复用首次请求的原始输入。
// 允许结构化字段保持零值，因为零值本身表示“交给文本解析或继承上一轮”。
func sameTurnRequest(saved memory.Turn, input agentcore.Input, fingerprint string) bool {
	var metadata struct {
		Fingerprint string `json:"_demand_request_sha256"`
	}
	if json.Unmarshal(saved.ResultJSON, &metadata) != nil {
		return false
	}
	return saved.Query == input.Query &&
		saved.BudgetCents == input.BudgetCents &&
		saved.MaxItems == input.MaxItems && metadata.Fingerprint == fingerprint
}

func (s *Service) planDemand(ctx context.Context, input agentcore.Input, patch demand.Patch) (*agentcore.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var queries []string
	prior := input.PriorIntent
	// Early records can contain only a title or partial numeric state. Preserve
	// their textual history until structured state has actually been established.
	if prior == nil || prior.Demand == nil && (prior.BudgetCents == 0 || prior.MaxItems == 0) {
		history, err := s.memory.History(ctx, input.UserId, input.ConversationId, 0)
		if err != nil {
			return nil, err // Planning cannot silently discard inherited conditions.
		}
		for _, msg := range history {
			if msg != nil && msg.Role == schema.User {
				queries = append(queries, msg.Content)
			}
		}
	}
	result, err := NewPlanner().ResolveDemand(input, queries, patch)
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	return result, err
}

// run 在编排结束后只校验一次；校验失败不能触发 fallback 绕过严格策略。
func (s *Service) run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	result, err := s.runCandidate(ctx, input)
	if err != nil {
		return nil, err
	}
	if s.finalizer != nil {
		if err := s.finalizer.Finalize(ctx, result); err != nil {
			return nil, safety.Protect(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, err := agentcore.NewConstraints(result.Intent)
	if err != nil {
		return nil, err
	}
	if err := limits.ValidateResult(result); err != nil {
		return nil, err
	}
	result.ToolsUsed = safety.ToolCalls(result.ToolsUsed)
	return result, nil
}

// runCandidate 按 primary 优先、失败降级的顺序生成待核验组合。
func (s *Service) runCandidate(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var queries []string
	// 旧的纯文本记忆可补充状态；已有结构化状态时不重复拉取整个窗口。
	if input.PriorIntent == nil && s.memory != nil {
		history, err := s.memory.History(ctx, input.UserId, input.ConversationId, 0)
		if err != nil {
			logx.WithContext(ctx).Errorw("load intent history failed", logx.Field("error_code", safety.ErrorCode(err)))
		} else {
			for _, msg := range history {
				if msg != nil && msg.Role == schema.User {
					queries = append(queries, msg.Content)
				}
			}
		}
	}
	intent, err := NewPlanner().Resolve(input, queries)
	if err != nil {
		return nil, err
	}
	// 只覆盖传给 Agent 的执行副本；Recommend/saveTurn 仍保存原始输入以判定幂等。
	input.BudgetCents, input.MaxItems = intent.BudgetCents, intent.MaxItems
	if s.primary == nil {
		return s.runChecked(ctx, s.fallback, input, intent)
	}

	result, err := s.runChecked(ctx, s.primary, input, intent)
	if err == nil {
		return result, nil
	}
	if agentcore.IsExecutionStopped(err) || errors.Is(err, agentcore.ErrContextTooLarge) || errors.Is(err, agentcore.ErrInvalidInput) {
		return nil, err
	}
	return s.fallbackAfterFailure(ctx, input, intent, err)
}

// runChecked 在共享编排边界校验两条路径；异常结果不会进入 SaveTurn。
func (s *Service) runChecked(ctx context.Context, runner agentcore.Agent, input agentcore.Input, intent agentcore.Intent) (*agentcore.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 每次执行隔离可变切片，primary 不能污染 fallback 的有效约束。
	prior := cloneIntent(intent)
	input.PriorIntent = &prior
	result, err := runner.Run(ctx, input)
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err != nil {
		return nil, err
	}
	limits, err := agentcore.NewConstraints(intent)
	if err != nil {
		return nil, err
	}
	if err := limits.ValidateResult(result); err != nil {
		return nil, err
	}
	result.Intent = cloneIntent(intent)
	result.ToolsUsed = safety.ToolCalls(result.ToolsUsed)
	return result, nil
}

// saveTurn 将领域结果序列化后，与当前结构化意图一并原子持久化。
func (s *Service) saveTurn(ctx context.Context, store memory.ConversationStore, input agentcore.Input, result *agentcore.Result, fingerprint string) (memory.Conversation, memory.Turn, error) {
	if err := ctx.Err(); err != nil {
		return memory.Conversation{}, memory.Turn{}, err
	}
	// Private idempotency metadata is created here, never supplied by a runner or
	// returned by public result/history mappers. Omission preserves legacy JSON.
	resultJSON, err := json.Marshal(struct {
		*agentcore.Result
		Fingerprint string `json:"_demand_request_sha256,omitempty"`
	}{result, fingerprint})
	if err != nil {
		return memory.Conversation{}, memory.Turn{}, fmt.Errorf("encode recommendation result: %w", err)
	}
	state := stateFromIntent(result.Intent)
	// Even a first-turn conflict locks this conversation into planning mode.
	// Otherwise a subsequent legacy call could bypass pending clarification.
	state.PlanningOnly = fingerprint != ""
	return store.SaveTurn(ctx, memory.SaveTurnReq{
		UserId: input.UserId, ConversationId: input.ConversationId, TurnId: input.TurnId,
		Title: result.ConversationTitle, Query: input.Query, BudgetCents: input.BudgetCents,
		MaxItems: input.MaxItems, Intent: state,
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
	result.ToolsUsed = safety.ToolCalls(result.ToolsUsed)
	return &result, nil
}

// stateFromIntent 提取需要跨轮长期保留的推荐约束。
func stateFromIntent(intent agentcore.Intent) memory.IntentState {
	return memory.IntentState{BudgetCents: intent.BudgetCents, MaxItems: intent.MaxItems,
		Keywords: append([]string(nil), intent.Keywords...), Preferences: append([]string(nil), intent.Preferences...), Demand: agentcore.CloneDemand(intent.Demand)}
}

// intentFromState 把持久化状态恢复为 Planner 可继承的上一轮意图。
func intentFromState(state memory.IntentState) agentcore.Intent {
	return agentcore.Intent{BudgetCents: state.BudgetCents, MaxItems: state.MaxItems,
		Keywords: append([]string(nil), state.Keywords...), Preferences: append([]string(nil), state.Preferences...), Demand: agentcore.CloneDemand(state.Demand)}
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
	if err := validateRequiredID("conversation_id", conversationId); err != nil {
		return memory.Conversation{}, nil, 0, false, err
	}
	store, ok := s.memory.(memory.ConversationStore)
	if !ok {
		return memory.Conversation{}, nil, 0, false, fmt.Errorf("conversation store is not configured")
	}
	return store.ListTurns(ctx, userId, conversationId, page, pageSize)
}

// DeleteConversation 删除当前用户拥有的指定会话及其轮次。
func (s *Service) DeleteConversation(ctx context.Context, userId, conversationId string) (bool, error) {
	if err := validateRequiredID("conversation_id", conversationId); err != nil {
		return false, err
	}
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
			logx.Field("conversation_id", safety.Label(input.ConversationId)),
			logx.Field("error_code", safety.ErrorCode(err)),
		)
	}
}

// conversationTitle 以首条用户问题作为稳定标题，并独立于滚动消息窗口持久化。
func (s *Service) conversationTitle(ctx context.Context, input agentcore.Input) string {
	candidate := shortTitle(input.Query)
	if store, ok := s.memory.(memory.ConversationStore); ok {
		conversation, exists, err := store.GetConversation(ctx, input.UserId, input.ConversationId)
		if err != nil {
			logx.WithContext(ctx).Errorw("load conversation title failed", logx.Field("user_id", safety.Label(input.UserId)), logx.Field("conversation_id", safety.Label(input.ConversationId)), logx.Field("error_code", safety.ErrorCode(err)))
		} else if exists && strings.TrimSpace(conversation.Title) != "" {
			return conversation.Title
		}
		return candidate
	}
	if s.memory != nil {
		title, err := s.memory.GetOrCreateTitle(ctx, input.UserId, input.ConversationId, candidate)
		if err != nil {
			logx.WithContext(ctx).Errorw("load or create conversation title failed", logx.Field("user_id", safety.Label(input.UserId)), logx.Field("conversation_id", safety.Label(input.ConversationId)), logx.Field("error_code", safety.ErrorCode(err)))
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
func (s *Service) fallbackAfterFailure(ctx context.Context, input agentcore.Input, intent agentcore.Intent, cause error) (*agentcore.Result, error) {
	result, err := s.runChecked(ctx, s.fallback, input, intent)
	if err != nil {
		return nil, err
	}
	detail := "primary execution failed; used validated fallback"
	if errors.Is(cause, agentcore.ErrUnsafeResult) {
		detail = "primary result rejected by constraints; used validated fallback"
	}
	result.ToolsUsed = append(result.ToolsUsed, agentcore.ToolCall{
		Name:    "primary." + safety.Label(s.primary.Name()),
		Success: false,
		Detail:  detail,
	})
	return result, nil
}
