// Package svc 封装 agent-rpc 的服务上下文与依赖组装。
package svc

import (
	"context"

	"budgetmatch-sim/infra/database"
	iredis "budgetmatch-sim/infra/redis"
	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/llm"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/agent/model/product_vectors"
	"budgetmatch-sim/services/rpc/mall/client/productservice"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/zrpc"
)

// ServiceContext 保存服务运行所需的全部依赖。
type ServiceContext struct {
	Config           config.Config           // Config 服务配置
	RecommendService *recommendagent.Service // RecommendService 推荐业务编排服务
	Syncer           *rag.Syncer             // Syncer 商品向量后台同步器，RAG 未启用时为 nil
}

// NewServiceContext 根据配置初始化服务上下文。
//
// 依赖降级矩阵（任意缺失都能启动）：
//   - 无 MallRpc：商品数据用内存 mock（配置了 mall 则绝不混用 mock）；
//   - 无 Embedding 或无 Database：RAG 关闭，provider 走关键词模式；
//   - 无 CacheRedis：会话记忆退回进程内实现；
//   - 无 Model：LLM Agent 不启用，推荐走确定性规则。
func NewServiceContext(c config.Config) *ServiceContext {
	var mallClient productservice.ProductService
	if c.MallConfigured() {
		mallClient = productservice.NewProductService(zrpc.MustNewClient(c.MallRpc))
	}

	productProvider := newProductProvider(mallClient)
	productProvider, syncer := maybeEnableRAG(c, mallClient, productProvider)

	bundleSelector := recommend.NewBundleSelector()
	mem := newMemoryManager(c)

	fallbackAgent := recommendagent.NewAgent(productProvider, bundleSelector)
	primaryAgent := newLLMAgent(c, productProvider, bundleSelector, mem)

	return &ServiceContext{
		Config:           c,
		RecommendService: recommendagent.NewService(fallbackAgent, primaryAgent, mem),
		Syncer:           syncer,
	}
}

// newProductProvider 按配置选择商品数据源：配置了 mall-rpc 用真实商品，否则用内存 mock。
// 配置了 mall 就不再混用 mock——mock 的商品 ID 在 mall 不存在，混入会推荐出不可下单的商品。
func newProductProvider(mallClient productservice.ProductService) tools.ProductProvider {
	if mallClient == nil {
		logx.Info("mallRpc not configured, product data falls back to in-memory mock")
		return tools.NewMockProductProvider()
	}
	return tools.NewMallProductProvider(mallClient)
}

// maybeEnableRAG 在依赖齐备时开启语义检索：建向量存储、启动后台同步，
// 并把关键词 provider 包装为"向量优先、关键词回退"的 RAG provider。
// 依赖不齐时原样返回入参 provider，并说明缺了什么。
func maybeEnableRAG(c config.Config, mallClient productservice.ProductService,
	fallback tools.ProductProvider) (tools.ProductProvider, *rag.Syncer) {
	if !c.RAGConfigured() {
		logx.Infow("rag disabled",
			logx.Field("database", c.Database.DSN != ""),
			logx.Field("embedding", c.Embedding.Enabled()),
			logx.Field("mall", mallClient != nil),
		)
		return fallback, nil
	}

	// 配置即意图：声明了 RAG 依赖却初始化失败，直接 panic 阻止带病启动。
	db, err := database.NewDatabase(c.Database)
	if err != nil {
		panic(err)
	}
	embedder, err := rag.NewEmbedder(context.Background(), c.Embedding)
	if err != nil {
		panic(err)
	}

	store, err := rag.NewStore(product_vectors.NewProductVectorsModel(db.DB()), embedder, c.RAG, c.Embedding.Dim())
	if err != nil {
		panic(err)
	}
	loader := rag.NewMallProductLoader(mallClient, c.RAG.Normalize().SyncPageSize)
	pipeline, err := rag.NewPipeline(loader, nil, rag.NewIndexer(store),
		product_vectors.NewProductVectorsModel(db.DB()), store.Fingerprint(c.Embedding.Model))
	if err != nil {
		panic(err)
	}

	syncer := rag.NewSyncer(pipeline, c.RAG.Normalize().SyncIntervalSeconds)
	syncer.Start()
	proc.AddShutdownListener(syncer.Stop)

	var verify tools.SkuGetter
	if c.RAG.VerifySku {
		verify = mallClient
	}
	logx.Info("rag enabled: semantic product retrieval over pgvector")
	return tools.NewRAGProductProvider(rag.NewRetriever(store), fallback, verify, c.RAG.Normalize().TopK), syncer
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
	return llm.NewAgent(model, productProvider, bundleSelector, c.MCP, c.FileTools).
		WithMemory(mem, c.Memory.Window())
}
