package config

import (
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/database"
	"budgetmatch-sim/infra/redis"

	"github.com/zeromicro/go-zero/gateway"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Gateway    gateway.GatewayConf `json:"gateway,optional"`
	Database   database.Config
	CacheRedis redis.Config        `json:"cacheRedis"`
	JwtAuth    auth.Config         `json:"jwtAuth"`
}
