package svc

import (
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/llm"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

type ServiceContext struct {
	Config      config.Config
	Agents      *agent.Registry
	ModelClient llm.Client
}

func NewServiceContext(c config.Config) *ServiceContext {
	modelClient, err := llm.NewClient(c.Model)
	if err != nil {
		panic(err)
	}

	productProvider := tools.NewMockProductProvider()
	bundleSelector := recommend.NewBundleSelector()

	registry := agent.NewRegistry()
	registry.MustRegister(recommendagent.NewAgent(productProvider, bundleSelector, c.MCP))

	return &ServiceContext{
		Config:      c,
		Agents:      registry,
		ModelClient: modelClient,
	}
}
