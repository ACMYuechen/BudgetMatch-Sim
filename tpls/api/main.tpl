// Code scaffolded by goctl. No recover, Safe to edit.

package main

import (
	"flag"
	"fmt"
    "github.com/zeromicro/go-zero/core/logx"
	{{.importPackages}}
)

var configFile = flag.String("f", "etc/config.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
    logx.AddGlobalFields(logx.Field("service_name", c.Log.ServiceName))

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	ctx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
