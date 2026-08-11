package config

import (
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/database"
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/model"
	"budgetmatch-sim/services/rpc/agent/internal/rag"

	"github.com/zeromicro/go-zero/zrpc"
)

// Config 是 agent-rpc 服务的总配置。
// Model/Embedding/Database/MallRpc/CacheRedis 均可选，任意缺失都能启动，
// 服务按可用依赖自动降级（见 svc.NewServiceContext 的组装逻辑）。
type Config struct {
	zrpc.RpcServerConf
	JwtAuth    auth.Config           `json:"jwtAuth"`             // JwtAuth JWT 认证配置
	Model      model.Config          `json:"model,optional"`      // Model LLM 模型配置
	Embedding  model.EmbeddingConfig `json:"embedding,optional"`  // Embedding 向量模型配置，未配置时 RAG 关闭
	MCP        mcp.Config            `json:"mcp,optional"`        // MCP MCP 服务器配置
	FileTools  filetools.Config      `json:"fileTools,optional"`  // FileTools LLM 文件工具的受限工作目录与访问限制
	CacheRedis iredis.Config         `json:"cacheRedis,optional"` // CacheRedis PostgreSQL 会话的一级缓存；无数据库时可独立保存短期记忆
	Memory     memory.Conf           `json:"memory,optional"`     // Memory 会话记忆行为配置（窗口/TTL）
	MallRpc    zrpc.RpcClientConf    `json:"mallRpc,optional"`    // MallRpc 商城 RPC 客户端，未配置时商品数据用内存 mock
	Database   database.Config       `json:"database,optional"`   // Database 会话持久化与商品向量表所在库；DSN 为空时记忆降级且 RAG 关闭
	RAG        rag.Config            `json:"rag,optional"`        // RAG 检索与同步行为配置
}

// MallConfigured 返回是否配置了 mall-rpc 数据源。
func (c Config) MallConfigured() bool {
	return len(c.MallRpc.Etcd.Hosts) > 0 || len(c.MallRpc.Endpoints) > 0 || c.MallRpc.Target != ""
}

// RAGConfigured 返回 RAG 链路的全部前置依赖是否就绪：
// 需要数据库（向量表）、embedding 模型（向量化）与 mall 数据源（可索引的真实商品）。
func (c Config) RAGConfigured() bool {
	return c.Database.DSN != "" && c.Embedding.Enabled() && c.MallConfigured()
}
