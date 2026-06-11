// Code scaffolded by goctl. No recover, Safe to edit.

package svc

import (
	{{.configImport}}

	"github.com/go-playground/validator/v10"
)

type ServiceContext struct {
	Config {{.config}}
	Validator *validator.Validate
	{{.middleware}}
}

func NewServiceContext(c {{.config}}) *ServiceContext {
	valid := validator.New(validator.WithRequiredStructEnabled())

	return &ServiceContext{
		Config:    c,
		Validator: valid,
		{{.middlewareAssignment}}
	}
}
