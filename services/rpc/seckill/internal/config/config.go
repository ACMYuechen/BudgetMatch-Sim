package config

import (
	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/database"
	"budgetmatch-sim/infra/redis"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config
	Redis    redis.Config
	Auth     auth.Config
}
