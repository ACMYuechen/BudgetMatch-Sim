func (m *default{{.upperStartCamelObject}}Model) FindOne(ctx context.Context, {{.lowerStartCamelPrimaryKey}} {{.dataType}}) (*{{.upperStartCamelObject}}, error) {
	model := &{{.upperStartCamelObject}}{}
    err := m.conn.WithContext(ctx).Where("id = ?", {{.lowerStartCamelPrimaryKey}}).First(model).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return model, nil
}

type {{.upperStartCamelObject}}ListReq struct {
    Page int `json:"page"` // Page number
    Size int `json:"size"` // Number of items per page
}
func (m *default{{.upperStartCamelObject}}Model) List(ctx context.Context, req {{.upperStartCamelObject}}ListReq) ([]{{.upperStartCamelObject}}, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

    total := int64(0)
    list := make([]{{.upperStartCamelObject}}, 0)
    session := m.conn.WithContext(ctx).Model(&{{.upperStartCamelObject}}{})
	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, err
	}

	offset := (req.Page - 1) * req.Size
	err = session.Limit(req.Size).Offset(offset).Find(&list).Error

	return list, total, err
}
