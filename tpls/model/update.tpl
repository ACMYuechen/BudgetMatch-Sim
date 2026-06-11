func (m *default{{.upperStartCamelObject}}Model) Update(ctx context.Context, data *{{.upperStartCamelObject}}) error {
	return m.conn.WithContext(ctx).Model(&{{.upperStartCamelObject}}{}).Where("id = ?", data.{{.upperStartCamelPrimaryKey}}).Updates(data).Error
}
