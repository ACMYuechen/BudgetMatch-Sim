package authservicelogic

import (
	"context"
	"testing"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/auth/internal/config"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"
)

// mockEmailUserModel 用于邮箱登录测试
type mockEmailUserModel struct {
	findByEmailFunc func(ctx context.Context, email string) (*user.Users, error)
}

func (m *mockEmailUserModel) CreateTable() error                                    { return nil }
func (m *mockEmailUserModel) Insert(ctx context.Context, data []*user.Users) error  { return nil }
func (m *mockEmailUserModel) InsertOne(ctx context.Context, data *user.Users) error { return nil }
func (m *mockEmailUserModel) List(ctx context.Context, req user.UsersListReq) ([]user.Users, int64, error) {
	return nil, 0, nil
}
func (m *mockEmailUserModel) FindOne(ctx context.Context, id string) (*user.Users, error) {
	return nil, nil
}
func (m *mockEmailUserModel) Update(ctx context.Context, data *user.Users) error { return nil }
func (m *mockEmailUserModel) Delete(ctx context.Context, id string) error        { return nil }
func (m *mockEmailUserModel) FindByIds(ctx context.Context, ids []string) ([]user.Users, error) {
	return nil, nil
}
func (m *mockEmailUserModel) FindByEmail(ctx context.Context, email string) (*user.Users, error) {
	if m.findByEmailFunc != nil {
		return m.findByEmailFunc(ctx, email)
	}
	return nil, nil
}
func (m *mockEmailUserModel) FindByUsername(ctx context.Context, username string) (*user.Users, error) {
	return nil, nil
}
func (m *mockEmailUserModel) ListByFilter(ctx context.Context, req user.UsersListFilterReq) ([]user.Users, int64, error) {
	return nil, 0, nil
}

// TestEmailLogin_Success 测试邮箱登录成功场景
// 验证正确邮箱和密码能够成功登录，返回正确的用户ID、Token和角色信息
func TestEmailLogin_Success(t *testing.T) {
	password := "correct-password"
	hashed, _ := auth.HashPassword(password)

	mockStore := &mockEmailUserModel{
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return &user.Users{
				Id:       "user-456",
				Username: "testuser",
				Email:    email,
				Password: hashed,
				Role:     role.RoleUser,
			}, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{
				Secret: "test-secret",
				Expire: 3600,
			},
		},
		UserStore: mockStore,
	}

	l := NewEmailLoginLogic(context.Background(), svcCtx)
	resp, err := l.EmailLogin(&pb.EmailLoginReq{
		Email:    "user@example.com",
		Password: password,
	})

	if err != nil {
		t.Fatalf("登录应该成功，但返回错误: %v", err)
	}
	if resp.UserId != "user-456" {
		t.Fatalf("user_id 不匹配: got %s, want user-456", resp.UserId)
	}
	if resp.Token == "" {
		t.Fatal("token 不应为空")
	}
	if resp.Role != int32(role.RoleUser) {
		t.Fatalf("role 不匹配: got %d, want %d", resp.Role, role.RoleUser)
	}
}

// TestEmailLogin_UserNotFound 测试邮箱不存在场景
// 验证使用不存在的邮箱登录时，返回 UserNotFound 错误
func TestEmailLogin_UserNotFound(t *testing.T) {
	mockStore := &mockEmailUserModel{
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return nil, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config:    config.Config{JwtAuth: auth.Config{Secret: "test", Expire: 3600}},
		UserStore: mockStore,
	}

	l := NewEmailLoginLogic(context.Background(), svcCtx)
	_, err := l.EmailLogin(&pb.EmailLoginReq{
		Email:    "nonexistent@example.com",
		Password: "any",
	})

	if err != errors.UserNotFound {
		t.Fatalf("期望 UserNotFound，但得到: %v", err)
	}
}

// TestEmailLogin_InvalidPassword 测试密码错误场景
// 验证使用正确邮箱但错误密码登录时，返回 InvalidPassword 错误
func TestEmailLogin_InvalidPassword(t *testing.T) {
	hashed, _ := auth.HashPassword("correct-password")

	mockStore := &mockEmailUserModel{
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return &user.Users{
				Id:       "user-456",
				Username: "testuser",
				Email:    email,
				Password: hashed,
				Role:     role.RoleUser,
			}, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config:    config.Config{JwtAuth: auth.Config{Secret: "test", Expire: 3600}},
		UserStore: mockStore,
	}

	l := NewEmailLoginLogic(context.Background(), svcCtx)
	_, err := l.EmailLogin(&pb.EmailLoginReq{
		Email:    "user@example.com",
		Password: "wrong-password",
	})

	if err != errors.InvalidPassword {
		t.Fatalf("期望 InvalidPassword，但得到: %v", err)
	}
}

// TestEmailLogin_InvalidEmailFormat 测试无效邮箱格式场景
// 验证邮箱登录逻辑不校验邮箱格式（由前端校验），无效邮箱会返回用户不存在错误
func TestEmailLogin_InvalidEmailFormat(t *testing.T) {
	// 邮箱登录逻辑不校验邮箱格式（由前端校验），此用例验证无效邮箱会返回用户不存在
	mockStore := &mockEmailUserModel{
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return nil, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config:    config.Config{JwtAuth: auth.Config{Secret: "test", Expire: 3600}},
		UserStore: mockStore,
	}

	l := NewEmailLoginLogic(context.Background(), svcCtx)
	_, err := l.EmailLogin(&pb.EmailLoginReq{
		Email:    "",
		Password: "any",
	})

	if err != errors.UserNotFound {
		t.Fatalf("空邮箱应返回 UserNotFound，但得到: %v", err)
	}
}
