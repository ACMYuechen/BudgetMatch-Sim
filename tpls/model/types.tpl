type (
	{{.lowerStartCamelObject}}Model interface{
		{{.method}}
	}

	default{{.upperStartCamelObject}}Model struct {
		conn *gorm.DB
		table string
	}

	{{.upperStartCamelObject}} struct {
		{{.fields}}
	}
)
