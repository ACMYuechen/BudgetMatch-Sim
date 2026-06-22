package config

import (
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/redis"

	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf
	Redis redis.Config `json:"redis"`
	Auth  auth.Config  `json:"auth"`

	// rpc 配置
	AuthRpc    zrpc.RpcClientConf `json:"authRpc"`
	SeckillRpc zrpc.RpcClientConf `json:"seckillRpc"`
	MallRpc    zrpc.RpcClientConf `json:"mallRpc"`
	AgentRpc   zrpc.RpcClientConf `json:"agentRpc"`
}
