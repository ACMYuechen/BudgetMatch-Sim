package config

import (
	"budgetmatch-sim/infra/alipay"
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/database"
	"budgetmatch-sim/infra/redis"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	JwtAuth    auth.Config `json:"jwtAuth"`
	Database   database.Config
	CacheRedis redis.Config  `json:"cacheRedis"`
	Alipay     alipay.Config `json:"alipay"`
}
