func (m *default{{.upperStartCamelObject}}Model) CreateTable() error {
	return m.conn.AutoMigrate(&{{.upperStartCamelObject}}{})
}

func (m *default{{.upperStartCamelObject}}Model) Insert(ctx context.Context, data []*{{.upperStartCamelObject}}) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *default{{.upperStartCamelObject}}Model) InsertOne(ctx context.Context, data *{{.upperStartCamelObject}}) error {
	return m.conn.WithContext(ctx).Create(data).Error
}
