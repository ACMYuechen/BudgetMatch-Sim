package recommend

import (
	"context"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
)

// Service 编排推荐流程。
//
// 它持有两个实现了 agentcore.Agent 的推荐器：
//   - primary 是首选编排器（通常是 Eino ReAct LLM Agent），未配置模型时为 nil；
//   - fallback 是确定性规则推荐 Agent，始终可用。
//
// primary 可用时优先使用；primary 执行失败则降级到 fallback，并记录失败原因。
// 这样 LLM 链路是真正的编排主入口，规则推荐只承担兜底职责，二者不再各跑一遍。
type Service struct {
	primary  agentcore.Agent
	fallback agentcore.Agent
}

// NewService 创建推荐编排服务。fallback 必填，primary 可为 nil。
func NewService(fallback, primary agentcore.Agent) *Service {
	return &Service{
		primary:  primary,
		fallback: fallback,
	}
}

// Recommend 执行推荐流程。
func (s *Service) Recommend(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if s == nil || s.fallback == nil {
		return nil, agentcore.ErrAgentNotFound
	}
	if s.primary == nil {
		return s.fallback.Run(ctx, input)
	}

	result, err := s.primary.Run(ctx, input)
	if err == nil {
		return result, nil
	}
	return s.fallbackAfterFailure(ctx, input, err)
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
