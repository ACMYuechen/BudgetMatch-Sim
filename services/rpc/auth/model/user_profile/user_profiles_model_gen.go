// Code scaffolded by goctl. Safe to edit.
// gorm

package user_profile

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	userProfilesModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*UserProfiles) error
		InsertOne(ctx context.Context, data *UserProfiles) error
		List(ctx context.Context, req UserProfilesListReq) ([]UserProfiles, int64, error)
		FindOne(ctx context.Context, userId string) (*UserProfiles, error)
		Update(ctx context.Context, data *UserProfiles) error
		Delete(ctx context.Context, userId string) error
	}

	defaultUserProfilesModel struct {
		conn  *gorm.DB
		table string
	}

	UserProfiles struct {
		UserId           string         `json:"user_id" gorm:"type:varchar(36);primaryKey;comment:用户ID"`
		RealName         string         `json:"real_name" gorm:"type:varchar(50);comment:真实姓名"`
		School           string         `json:"school" gorm:"type:varchar(100);comment:学校"`
		Major            string         `json:"major" gorm:"type:varchar(100);comment:专业"`
		Grade            string         `json:"grade" gorm:"type:varchar(20);comment:年级"`
		Gender           int64          `json:"gender" gorm:"type:int;default:0;comment:性别 0:未设置 1:男 2:女"`
		ExpectedCity     string         `json:"expected_city" gorm:"type:varchar(100);comment:期望城市"`
		ExpectedPosition string         `json:"expected_position" gorm:"type:varchar(100);comment:期望岗位"`
		SelfIntroduction string         `json:"self_introduction" gorm:"type:text;comment:自我介绍"`
		CreatedAt        time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt        time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt        gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}
)

func newUserProfilesModel(conn *gorm.DB) *defaultUserProfilesModel {
	return &defaultUserProfilesModel{
		conn:  conn,
		table: `"public"."user_profiles"`,
	}
}
func (m *defaultUserProfilesModel) Delete(ctx context.Context, userId string) error {
	return m.conn.WithContext(ctx).Where("user_id = ?", userId).Delete(&UserProfiles{}).Error
}

func (m *defaultUserProfilesModel) FindOne(ctx context.Context, userId string) (*UserProfiles, error) {
	model := &UserProfiles{}
	err := m.conn.WithContext(ctx).Where("user_id = ?", userId).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

type UserProfilesListReq struct {
	Page int `json:"page"` // Page number
	Size int `json:"size"` // Number of items per page
}

func (m *defaultUserProfilesModel) List(ctx context.Context, req UserProfilesListReq) ([]UserProfiles, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]UserProfiles, 0)
	session := m.conn.WithContext(ctx).Model(&UserProfiles{})
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

func (m *defaultUserProfilesModel) CreateTable() error {
	return m.conn.AutoMigrate(&UserProfiles{})
}

func (m *defaultUserProfilesModel) Insert(ctx context.Context, data []*UserProfiles) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultUserProfilesModel) InsertOne(ctx context.Context, data *UserProfiles) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultUserProfilesModel) Update(ctx context.Context, data *UserProfiles) error {
	return m.conn.WithContext(ctx).Save(data).Error
}

func (m *defaultUserProfilesModel) tableName() string {
	return m.table
}
