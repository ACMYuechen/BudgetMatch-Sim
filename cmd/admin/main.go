package main

import (
	"flag"

	"budgetmatch-sim/cmd/admin/internal/config"
	"budgetmatch-sim/cmd/admin/internal/handler"
	"budgetmatch-sim/cmd/admin/internal/svc"

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

	// 创建REST服务器
	server := rest.MustNewServer(c.RestConf)
	sg.Add(server)

	// 初始化服务上下文（admin 不直接访问 DB，所有数据操作通过 auth-rpc）
	serverCtx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, serverCtx)

	// 启动服务
	logx.Infof("Starting admin server at %s:%d...", c.Host, c.Port)
	sg.Start()
}
