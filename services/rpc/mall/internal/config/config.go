package config

import (
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/database"
	"budgetmatch-sim/infra/redis"
	"budgetmatch-sim/infra/rocketmq"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Database   database.Config `json:"database"`
	CacheRedis redis.Config    `json:"cacheRedis"`
	JwtAuth    auth.Config     `json:"jwtAuth"`
	RocketMQ   rocketmq.Config `json:"rocketmq"`
}
