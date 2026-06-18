package config

import (
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/modelconfig"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Model modelconfig.Config `json:"model,optional"`
	MCP   mcp.Config         `json:"mcp,optional"`
}
