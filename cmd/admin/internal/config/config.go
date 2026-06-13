package config

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf

	// 认证中心 RPC 客户端配置
	AuthRpc zrpc.RpcClientConf `json:"authRpc"`

	// 秒杀服务 RPC 客户端配置
	SeckillRpc zrpc.RpcClientConf `json:"seckillRpc"`
}
