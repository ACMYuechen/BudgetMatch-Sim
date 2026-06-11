// Code scaffolded by goctl. No recover, Safe to edit.

package config

import {{.authImport}}

type Config struct {
	rest.RestConf
	{{.auth}}
	{{.jwtTrans}}
}
