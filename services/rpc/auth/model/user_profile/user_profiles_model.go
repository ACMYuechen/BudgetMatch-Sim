// Code scaffolded by goctl. Safe to edit.
// gorm

package user_profile

import (
	"context"

	"gorm.io/gorm"
)

var _ UserProfilesModel = (*customUserProfilesModel)(nil)

type (
	// UserProfilesModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUserProfilesModel.
	UserProfilesModel interface {
		userProfilesModel
		FindByUserId(ctx context.Context, userId string) (*UserProfiles, error)
		Upsert(ctx context.Context, data *UserProfiles) error
	}

	customUserProfilesModel struct {
		*defaultUserProfilesModel
	}
)

// NewUserProfilesModel returns a model for the database table.
func NewUserProfilesModel(conn *gorm.DB) UserProfilesModel {
	return &customUserProfilesModel{
		defaultUserProfilesModel: newUserProfilesModel(conn),
	}
}

func (m *customUserProfilesModel) FindByUserId(ctx context.Context, userId string) (*UserProfiles, error) {
	return m.FindOne(ctx, userId)
}

func (m *customUserProfilesModel) Upsert(ctx context.Context, data *UserProfiles) error {
	return m.Update(ctx, data)
}
