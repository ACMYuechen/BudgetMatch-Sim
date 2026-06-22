// Package svc 封装 agent-rpc 的服务上下文与依赖组装。
package svc

import (
	"context"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/llm"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// ServiceContext 保存服务运行所需的全部依赖。
type ServiceContext struct {
	Config           config.Config           // Config 服务配置
	RecommendService *recommendagent.Service // RecommendService 推荐业务编排服务
}

// NewServiceContext 根据配置初始化服务上下文。
// 组装确定性规则推荐 Agent（兜底）与可选的 Eino ReAct LLM Agent（首选编排器）。
func NewServiceContext(c config.Config) *ServiceContext {
	productProvider := tools.NewMockProductProvider()
	bundleSelector := recommend.NewBundleSelector()

	fallbackAgent := recommendagent.NewAgent(productProvider, bundleSelector)
	primaryAgent := newLLMAgent(c, productProvider, bundleSelector)

	return &ServiceContext{
		Config:           c,
		RecommendService: recommendagent.NewService(fallbackAgent, primaryAgent),
	}
}

// newLLMAgent 根据配置创建 Eino ReAct LLM 推荐 Agent。
// 未配置模型时返回 nil（接口类型的 nil 由 Service 直接走规则兜底）。
func newLLMAgent(c config.Config, productProvider tools.ProductProvider, bundleSelector *recommend.BundleSelector) agentcore.Agent {
	model, err := llm.NewChatModel(context.Background(), c.Model)
	if err != nil {
		panic(err)
	}
	if model == nil {
		return nil
	}
	return llm.NewAgent(model, productProvider, bundleSelector, c.MCP)
}
