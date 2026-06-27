package config

import (
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/model"

	"github.com/zeromicro/go-zero/zrpc"
)

// Config 是 agent-rpc 服务的总配置，包含 RPC 服务、模型与 MCP 配置。
type Config struct {
	zrpc.RpcServerConf
	Model model.Config `json:"model,optional"` // Model LLM 模型配置
	MCP   mcp.Config   `json:"mcp,optional"`   // MCP MCP 服务器配置
}
