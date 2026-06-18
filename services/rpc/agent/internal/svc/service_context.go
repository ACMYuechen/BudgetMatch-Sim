package svc

import (
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	recommendflow "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/flow"
	recommendtoolkit "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/llm"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

type ServiceContext struct {
	Config        config.Config
	Agents        *agent.Registry
	ModelClient   llm.Client
	RecommendFlow *recommendflow.Runner
}

func NewServiceContext(c config.Config) *ServiceContext {
	modelClient, err := llm.NewClient(c.Model)
	if err != nil {
		panic(err)
	}

	productProvider := tools.NewMockProductProvider()
	bundleSelector := recommend.NewBundleSelector()
	toolExecutor := recommendtoolkit.NewExecutor(productProvider, bundleSelector).WithMCP(c.MCP)

	registry := agent.NewRegistry()
	registry.MustRegister(recommendagent.NewAgent(productProvider, bundleSelector, c.MCP))

	return &ServiceContext{
		Config:        c,
		Agents:        registry,
		ModelClient:   modelClient,
		RecommendFlow: recommendflow.NewRunner(modelClient, toolExecutor),
	}
}
