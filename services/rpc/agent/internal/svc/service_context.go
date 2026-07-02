// Package svc 封装 agent-rpc 的服务上下文与依赖组装。
package svc

import (
	"context"

	iredis "budgetmatch-sim/infra/redis"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/llm"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/client/productservice"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

// ServiceContext 保存服务运行所需的全部依赖。
type ServiceContext struct {
	Config           config.Config           // Config 服务配置
	RecommendService *recommendagent.Service // RecommendService 推荐业务编排服务
}

// NewServiceContext 根据配置初始化服务上下文。
// 组装确定性规则推荐 Agent（兜底）、可选的 Eino ReAct LLM Agent（首选编排器）与会话记忆。
func NewServiceContext(c config.Config) *ServiceContext {
	productProvider := newProductProvider(c)
	bundleSelector := recommend.NewBundleSelector()
	mem := newMemoryManager(c)

	fallbackAgent := recommendagent.NewAgent(productProvider, bundleSelector)
	primaryAgent := newLLMAgent(c, productProvider, bundleSelector, mem)

	return &ServiceContext{
		Config:           c,
		RecommendService: recommendagent.NewService(fallbackAgent, primaryAgent, mem),
	}
}

// newProductProvider 按配置选择商品数据源：配置了 mall-rpc 用真实商品，否则用内存 mock。
// 配置了 mall 就不再混用 mock——mock 的商品 ID 在 mall 不存在，混入会推荐出不可下单的商品。
func newProductProvider(c config.Config) tools.ProductProvider {
	if !c.MallConfigured() {
		logx.Info("mallRpc not configured, product data falls back to in-memory mock")
		return tools.NewMockProductProvider()
	}
	return tools.NewMallProductProvider(productservice.NewProductService(zrpc.MustNewClient(c.MallRpc)))
}

// newMemoryManager 根据配置创建会话记忆：
// 配置了 CacheRedis 用 Redis 实现（多实例共享），否则退回进程内实现（本地开发）。
// Redis 配置了却连不上视为部署错误，直接 panic 阻止带病启动。
func newMemoryManager(c config.Config) memory.Manager {
	if c.CacheRedis.Address == "" {
		logx.Info("cacheRedis not configured, conversation memory falls back to in-process store")
		return memory.NewInMemory(c.Memory)
	}
	rdb, err := iredis.NewRedisDB(c.CacheRedis)
	if err != nil {
		panic(err)
	}
	return memory.NewRedis(rdb.Client(), c.Memory)
}

// newLLMAgent 根据配置创建 Eino ReAct LLM 推荐 Agent。
// 未配置模型时返回 nil（接口类型的 nil 由 Service 直接走规则兜底）。
func newLLMAgent(c config.Config, productProvider tools.ProductProvider, bundleSelector *recommend.BundleSelector, mem memory.Manager) agentcore.Agent {
	model, err := llm.NewChatModel(context.Background(), c.Model)
	if err != nil {
		panic(err)
	}
	if model == nil {
		return nil
	}
	return llm.NewAgent(model, productProvider, bundleSelector, c.MCP).
		WithMemory(mem, c.Memory.Window())
}
