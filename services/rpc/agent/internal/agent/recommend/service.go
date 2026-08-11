package recommend

import (
	"context"
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
// 成功返回前都写入本轮问答对，保证降级轮次的历史没有空洞；
// Agent 实现只读历史不写历史，避免多写入点带来的重复或缺失。
type Service struct {
	primary  agentcore.Agent
	fallback agentcore.Agent
	memory   memory.Manager
}

// NewService 创建推荐编排服务。fallback 必填，primary 与 mem 可为 nil（nil 记忆表示无多轮能力）。
func NewService(fallback, primary agentcore.Agent, mem memory.Manager) *Service {
	return &Service{
		primary:  primary,
		fallback: fallback,
		memory:   mem,
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

	result, err := s.run(ctx, input)
	if err != nil {
		logx.WithContext(ctx).Errorw("recommendation failed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId), logx.Field("error", err.Error()))
		return nil, err
	}

	result.ConversationId = input.ConversationId
	result.ConversationTitle = s.conversationTitle(ctx, input)
	s.remember(ctx, input, result)
	logx.WithContext(ctx).Infow("recommendation completed", logx.Field("user_id", input.UserId), logx.Field("conversation_id", input.ConversationId))
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
	return s.fallbackAfterFailure(ctx, input, err)
}

// remember 把本轮问答对写入会话记忆。
//
//   - user 消息存原始 Query：意图脚手架等增强文本只存在于单次模型输入中，入库会逐轮重复污染窗口；
//   - assistant 消息存 Summary：它永远非空（无模型文本时有确定性兜底摘要）、与用户所见一致，
//     且 fallback 路径没有模型原文，统一存 Summary 让两条路径写入逻辑相同。
//
// 记忆写入失败只记日志不阻断请求——推荐结果本身仍然有效。
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
