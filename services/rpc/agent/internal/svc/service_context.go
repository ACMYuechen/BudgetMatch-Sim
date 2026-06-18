package svc

import (
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	recommendflow "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/flow"
	recommendprompt "budgetmatch-sim/services/rpc/agent/internal/agent/recommend/prompt"
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
	PromptBuilder *recommendprompt.Builder
	RecommendFlow *recommendflow.Runner
}

func NewServiceContext(c config.Config) *ServiceContext {
	modelClient, err := llm.NewClient(c.Model)
	if err != nil {
		panic(err)
	}

	productProvider := tools.NewMockProductProvider()
	bundleSelector := recommend.NewBundleSelector()
	newToolExecutor := func() *recommendtoolkit.Executor {
		return recommendtoolkit.NewExecutor(productProvider, bundleSelector).WithMCP(c.MCP)
	}

	registry := agent.NewRegistry()
	registry.MustRegister(recommendagent.NewAgent(productProvider, bundleSelector))

	return &ServiceContext{
		Config:        c,
		Agents:        registry,
		ModelClient:   modelClient,
		PromptBuilder: recommendprompt.NewBuilder(),
		RecommendFlow: recommendflow.NewRunnerWithFactory(modelClient, newToolExecutor),
	}
}

func (s *ServiceContext) RecommendFlowEnabled() bool {
	return s != nil && s.Config.Model.Enabled() && s.ModelClient != nil && s.ModelClient.Name() != "noop"
}
