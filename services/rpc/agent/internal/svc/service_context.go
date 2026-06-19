// Package svc 封装 agent-rpc 的服务上下文与依赖组装。
package svc

import (
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	recommendeino "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/eino"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// ServiceContext 保存服务运行所需的全部依赖。
type ServiceContext struct {
	Config          config.Config         // Config 服务配置
	Agents          *agent.Registry       // Agents Agent 注册表
	RecommendRunner *recommendeino.Runner // RecommendRunner Eino 推荐运行时
}

// NewServiceContext 根据配置初始化服务上下文，包括模型、商品提供者、Agent 注册表与运行时。
func NewServiceContext(c config.Config) *ServiceContext {
	model, err := recommendeino.NewChatModel(c.Model)
	if err != nil {
		panic(err)
	}

	productProvider := tools.NewMockProductProvider()
	bundleSelector := recommend.NewBundleSelector()

	registry := agent.NewRegistry()
	registry.MustRegister(recommendagent.NewAgent(productProvider, bundleSelector))

	var recommendRunner *recommendeino.Runner
	if model != nil {
		recommendRunner = recommendeino.NewRunner(model, productProvider, bundleSelector, c.MCP)
	}

	return &ServiceContext{
		Config:          c,
		Agents:          registry,
		RecommendRunner: recommendRunner,
	}
}

// RecommendRuntimeEnabled 判断 Eino 推荐运行时是否可用。
func (s *ServiceContext) RecommendRuntimeEnabled() bool {
	return s != nil && s.Config.Model.Enabled() && s.RecommendRunner != nil && s.RecommendRunner.Enabled()
}
