package config

import (
	"fmt"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/database"
	"budgetmatch-sim/infra/interceptor"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/model"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/pb"
	"budgetmatch-sim/services/rpc/agent/streamcontract"

	"github.com/zeromicro/go-zero/zrpc"
)

// Config 是 agent-rpc 服务的总配置。
// Model/Embedding/Database/MallRpc/CacheRedis 均可选，任意缺失都能启动，
// 服务按可用依赖自动降级（见 svc.NewServiceContext 的组装逻辑）。
// 已声明 RAG 依赖时，IndexAuth 缺失或不合法会阻止启动，不静默降级。
// 显式选择混合检索时要求完整 RAG 依赖，不适用缺失依赖时的演示降级。
type Config struct {
	zrpc.RpcServerConf
	JwtAuth         auth.Config           `json:"jwtAuth"`             // JwtAuth JWT 认证配置
	Model           model.Config          `json:"model,optional"`      // Model LLM 模型配置
	Embedding       model.EmbeddingConfig `json:"embedding,optional"`  // Embedding 向量模型配置，未配置时 RAG 关闭
	MCP             mcp.Config            `json:"mcp,optional"`        // MCP MCP 服务器配置
	FileTools       filetools.Config      `json:"fileTools,optional"`  // FileTools LLM 文件工具的受限工作目录与访问限制
	CacheRedis      iredis.Config         `json:"cacheRedis,optional"` // CacheRedis PostgreSQL 会话的一级缓存；无数据库时可独立保存短期记忆
	Memory          memory.Conf           `json:"memory,optional"`     // Memory 会话记忆行为配置（窗口/TTL）
	MallRpc         zrpc.RpcClientConf    `json:"mallRpc,optional"`    // MallRpc 商城 RPC 客户端，未配置时商品数据用内存 mock
	Database        database.Config       `json:"database,optional"`   // Database 会话持久化与商品向量表所在库；DSN 为空时记忆降级且 RAG 关闭
	RAG             rag.Config            `json:"rag,optional"`        // RAG 检索与同步行为配置
	IndexAuth       serviceauth.Config    `json:"indexAuth,optional"`  // IndexAuth 后台索引专用凭据，不用于在线用户搜索
	DemandExecution DemandExecutionConfig `json:"demandExecution,optional"`
}

type DemandExecutionConfig struct {
	Mode      string `json:"mode,optional"`      // empty/disabled, isolated demo, or explicit mall.
	Retrieval string `json:"retrieval,optional"` // empty/keyword preserves baseline; rag explicitly shares RAG strategy.
}

// RPCAuthConfig shares the ordinary user policy across unary and streaming
// recommendation. Index service credentials never authorize online requests.
func (c Config) RPCAuthConfig() interceptor.AuthConfig {
	return interceptor.AuthConfig{Secret: c.JwtAuth.Secret,
		StreamMaxDurations: map[string]time.Duration{
			pb.RecommendService_RecommendStream_FullMethodName: streamcontract.MaxDuration,
		}}
}

// ValidateDemandExecution must run before any external clients or database I/O.
func (c Config) ValidateDemandExecution() error {
	if c.DemandExecution.Retrieval != "" && c.DemandExecution.Retrieval != "keyword" && c.DemandExecution.Retrieval != "rag" {
		return fmt.Errorf("unsupported demand retrieval mode")
	}
	switch c.DemandExecution.Mode {
	case "", "disabled":
		return nil
	case "demo":
		if !c.MallConfigured() && c.DemandExecution.Retrieval != "rag" {
			return nil
		}
	case "mall":
		if c.MallConfigured() {
			if c.DemandExecution.Retrieval == "rag" && (!c.RAGConfigured() || c.RAG.TopK < 0 || c.RAG.Normalize().TopK > rag.MaxHybridOutput) {
				return fmt.Errorf("demand rag retrieval requires Mall, database, embedding and TopK within 1..32")
			}
			return nil
		}
	}
	return fmt.Errorf("demand execution requires disabled, demo without Mall, or mall with Mall configured")
}

// ValidateRetrieval rejects typos and unmet experimental dependencies before I/O.
func (c Config) ValidateRetrieval() error {
	if err := c.RAG.Retrieval.Validate(c.RAG.TopK); err != nil {
		return err
	}
	if c.RAG.Retrieval.Normalize().Strategy == rag.StrategyHybridRRF && !c.RAGConfigured() {
		return fmt.Errorf("hybrid retrieval requires configured Mall, database and embedding")
	}
	return nil
}

// MallConfigured 返回是否配置了 mall-rpc 数据源。
func (c Config) MallConfigured() bool {
	return len(c.MallRpc.Etcd.Hosts) > 0 || len(c.MallRpc.Endpoints) > 0 || c.MallRpc.Target != ""
}

// RAGConfigured 返回是否声明数据库、embedding 和 Mall 数据源。
// 这表示启用意图，不代表依赖健康或鉴权就绪；凭据另由 ValidateIndexAuth 强制校验。
func (c Config) RAGConfigured() bool {
	return c.Database.DSN != "" && c.Embedding.Enabled() && c.MallConfigured()
}

// ValidateIndexAuth 不把缺少凭据的已配置 RAG 静默降级，也不尝试用户或 Payment 密钥。
func (c Config) ValidateIndexAuth() error {
	if !c.RAGConfigured() {
		return nil
	}
	return serviceauth.ValidateDedicatedSecret(c.IndexAuth.Secret, c.JwtAuth.Secret)
}
