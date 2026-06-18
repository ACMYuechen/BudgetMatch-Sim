package config

import (
	"budgetmatch-sim/services/rpc/agent/internal/llm"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Model llm.Config `json:"model,optional"`
	MCP   mcp.Config `json:"mcp,optional"`
}
