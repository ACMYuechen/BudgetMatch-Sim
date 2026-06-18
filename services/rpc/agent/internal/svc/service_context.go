package svc

import (
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	recommendeino "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/eino"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

type ServiceContext struct {
	Config          config.Config
	Agents          *agent.Registry
	RecommendRunner *recommendeino.Runner
}

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

func (s *ServiceContext) RecommendRuntimeEnabled() bool {
	return s != nil && s.Config.Model.Enabled() && s.RecommendRunner != nil && s.RecommendRunner.Enabled()
}
