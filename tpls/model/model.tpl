// Code scaffolded by goctl. Only init once, Safe to edit.
// gorm

package {{.pkg}}

import "gorm.io/gorm"

var _ {{.upperStartCamelObject}}Model = (*custom{{.upperStartCamelObject}}Model)(nil)

type (
	// {{.upperStartCamelObject}}Model 原生 DB 接口，不走缓存
	{{.upperStartCamelObject}}Model interface {
		{{.lowerStartCamelObject}}Model
	}

	custom{{.upperStartCamelObject}}Model struct {
		*default{{.upperStartCamelObject}}Model
	}
)

// New{{.upperStartCamelObject}}Model 创建纯 DB model
func New{{.upperStartCamelObject}}Model(conn *gorm.DB) {{.upperStartCamelObject}}Model {
	return &custom{{.upperStartCamelObject}}Model{
		default{{.upperStartCamelObject}}Model: new{{.upperStartCamelObject}}Model(conn),
	}
}
