package config

import (
	iredis "budgetmatch-sim/infra/redis"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/model"

	"github.com/zeromicro/go-zero/zrpc"
)

// Config 是 agent-rpc 服务的总配置，包含 RPC 服务、模型、MCP 与会话记忆配置。
type Config struct {
	zrpc.RpcServerConf
	Model      model.Config  `json:"model,optional"`      // Model LLM 模型配置
	MCP        mcp.Config    `json:"mcp,optional"`        // MCP MCP 服务器配置
	CacheRedis iredis.Config `json:"cacheRedis,optional"` // CacheRedis 会话记忆存储，Address 为空时用进程内记忆
	Memory     memory.Conf   `json:"memory,optional"`     // Memory 会话记忆行为配置（窗口/TTL）
}
