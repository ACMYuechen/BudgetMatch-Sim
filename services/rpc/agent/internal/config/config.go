package config

import (
	"budgetmatch-sim/services/rpc/agent/internal/mcp"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Model ModelConfig `json:"model,optional"`
	MCP   mcp.Config  `json:"mcp,optional"`
}

type ModelConfig struct {
	Provider string `json:"provider,optional"`
	Model    string `json:"model,optional"`
	BaseURL  string `json:"baseUrl,optional"`
	APIKey   string `json:"apiKey,optional"`
}
