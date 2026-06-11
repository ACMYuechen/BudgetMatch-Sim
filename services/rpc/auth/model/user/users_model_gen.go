// Code scaffolded by goctl. Safe to edit.
// gorm

package user

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	usersModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*Users) error
		InsertOne(ctx context.Context, data *Users) error
		List(ctx context.Context, req UsersListReq) ([]Users, int64, error)
		FindOne(ctx context.Context, id string) (*Users, error)
		Update(ctx context.Context, data *Users) error
		Delete(ctx context.Context, id string) error
	}

	defaultUsersModel struct {
		conn  *gorm.DB
		table string
	}

	Users struct {
		Id        string         `json:"id" gorm:"type:varchar(36);primaryKey;comment:用户ID"`
		Username  string         `json:"username" gorm:"type:varchar(50);uniqueIndex;not null;comment:用户名"`
		Avatar    string         `json:"avatar" gorm:"type:varchar(255);comment:头像URL"`
		Phone     string         `json:"phone" gorm:"type:varchar(20);index;default:'';comment:手机号"`
		Email     string         `json:"email" gorm:"type:varchar(100);index;default:'';comment:邮箱"`
		Password  string         `json:"password" gorm:"type:varchar(255);not null;comment:密码"`
		Role      int64          `json:"role" gorm:"type:int;default:1;comment:角色，1:普通用户 2:管理员"`
		Status    int64          `json:"status" gorm:"type:int;default:1;comment:状态，1:启用 2:禁用 3:待注销 4:已注销"`
		Remark    string         `json:"remark" gorm:"type:varchar(255);comment:备注"`
		CreatedAt time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}
)

func newUsersModel(conn *gorm.DB) *defaultUsersModel {
	return &defaultUsersModel{
		conn:  conn,
		table: `"public"."users"`,
	}
}
func (m *defaultUsersModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&Users{}).Error
}

func (m *defaultUsersModel) FindOne(ctx context.Context, id string) (*Users, error) {
	model := &Users{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

type UsersListReq struct {
	Page int `json:"page"` // Page number
	Size int `json:"size"` // Number of items per page
}

func (m *defaultUsersModel) List(ctx context.Context, req UsersListReq) ([]Users, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]Users, 0)
	session := m.conn.WithContext(ctx).Model(&Users{})
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

func (m *defaultUsersModel) CreateTable() error {
	return m.conn.AutoMigrate(&Users{})
}

func (m *defaultUsersModel) Insert(ctx context.Context, data []*Users) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultUsersModel) InsertOne(ctx context.Context, data *Users) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultUsersModel) Update(ctx context.Context, data *Users) error {
	return m.conn.WithContext(ctx).Model(&Users{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultUsersModel) tableName() string {
	return m.table
}
