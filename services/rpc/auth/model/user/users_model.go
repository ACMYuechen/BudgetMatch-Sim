// Code scaffolded by goctl. Safe to edit.
// gorm

package user

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var _ UsersModel = (*customUsersModel)(nil)

type (
	// UsersModel 原生 DB 接口，不走缓存
	UsersModel interface {
		usersModel
		FindByIds(ctx context.Context, ids []string) ([]Users, error)
		FindByEmail(ctx context.Context, email string) (*Users, error)
		FindByUsername(ctx context.Context, username string) (*Users, error)
		ListByFilter(ctx context.Context, req UsersListFilterReq) ([]Users, int64, error)
	}

	UsersListFilterReq struct {
		Page   int
		Size   int
		Status int64
		Role      int64          `json:"role" gorm:"type:int;default:100;comment:角色身份，数字标识（100为普通用户）"`
	}

	customUsersModel struct {
		*defaultUsersModel
	}
)

// NewUsersModel 创建纯 DB model
func NewUsersModel(conn *gorm.DB) UsersModel {
	return &customUsersModel{
		defaultUsersModel: newUsersModel(conn),
	}
}

func (m *customUsersModel) FindByIds(ctx context.Context, ids []string) ([]Users, error) {
	models := make([]Users, 0)
	if len(ids) == 0 {
		return models, nil
	}
	err := m.conn.WithContext(ctx).Where("id IN (?)", ids).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}

func (m *customUsersModel) FindByEmail(ctx context.Context, email string) (*Users, error) {
	model := &Users{}
	err := m.conn.WithContext(ctx).Where("email = ?", email).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *customUsersModel) FindByUsername(ctx context.Context, username string) (*Users, error) {
	model := &Users{}
	err := m.conn.WithContext(ctx).Where("username = ?", username).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

// CreateTable 覆盖默认实现，避免多个服务同时 AutoMigrate 冲突
func (m *customUsersModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&Users{}) {
		return m.conn.Migrator().CreateTable(&Users{})
	}
	return nil
}

func (m *customUsersModel) ListByFilter(ctx context.Context, req UsersListFilterReq) ([]Users, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]Users, 0)
	session := m.conn.WithContext(ctx).Model(&Users{})

	if req.Status > 0 {
		session = session.Where("status = ?", req.Status)
	}
	if req.Role > 0 {
		session = session.Where("role = ?", req.Role)
	}

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (req.Page - 1) * req.Size
	err = session.Order("created_at DESC").Limit(req.Size).Offset(offset).Find(&list).Error

	return list, total, err
}
