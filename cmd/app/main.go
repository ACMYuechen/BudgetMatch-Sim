package main

import (
	"budgetmatch-sim/cmd/app/internal/config"
	"budgetmatch-sim/cmd/app/internal/handler"
	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/infra/redis"
	"flag"

	inframiddleware "budgetmatch-sim/infra/middleware"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/config.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())

	// 设置日志
	logx.MustSetup(c.Log)
	logx.AddGlobalFields(logx.Field("service_name", c.Log.ServiceName))

	// 创建服务组
	sg := service.NewServiceGroup()
	defer sg.Stop()

	// 初始化Redis
	rds, err := redis.NewRedisDB(c.Redis)
	if err != nil {
		logx.Must(err)
	}
	defer rds.Close()

	// 创建REST服务器
	server := rest.MustNewServer(c.RestConf)
	// 注册全局请求日志中间件（最外层，包裹所有路由与业务中间件）
	server.Use(inframiddleware.NewLoggingMiddleware(c.Auth.Secret).Handle)
	sg.Add(server)

	// 初始化服务上下文
	serverCtx := svc.NewServiceContext(c, rds.Client())
	handler.RegisterHandlers(server, serverCtx)

	// 注册服务
	// sg.Add(...)

	// 启动服务
	logx.Infof("Starting app server at %s:%d...", c.Host, c.Port)
	sg.Start()
}
