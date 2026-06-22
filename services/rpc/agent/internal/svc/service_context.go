// Package svc 封装 agent-rpc 的服务上下文与依赖组装。
package svc

import (
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	recommendeino "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/eino"
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
// 这里负责组装规则推荐 Agent、可选 Eino 精修器，以及推荐业务编排服务。
func NewServiceContext(c config.Config) *ServiceContext {
	productProvider := tools.NewMockProductProvider()
	bundleSelector := recommend.NewBundleSelector()

	draftAgent := recommendagent.NewAgent(productProvider, bundleSelector)
	refiner := newRecommendRefiner(c, productProvider, bundleSelector)

	return &ServiceContext{
		Config:           c,
		RecommendService: recommendagent.NewService(draftAgent, refiner),
	}
}

// newRecommendRefiner 根据配置创建推荐精修器。
// 当前实现使用 Eino ReAct；未配置模型时返回 nil，推荐流程只走规则兜底。
func newRecommendRefiner(c config.Config, productProvider tools.ProductProvider, bundleSelector *recommend.BundleSelector) recommendagent.Refiner {
	model, err := recommendeino.NewChatModel(c.Model)
	if err != nil {
		panic(err)
	}
	if model == nil {
		return nil
	}
	return recommendeino.NewRunner(model, productProvider, bundleSelector, c.MCP)
}
