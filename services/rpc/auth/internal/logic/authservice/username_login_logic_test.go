package authservicelogic

import (
	"context"
	"testing"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/config"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"
)

// mockUsersModel 是 UsersModel 的 mock 实现，仅用于单元测试
type mockUsersModel struct {
	findByUsernameFunc func(ctx context.Context, username string) (*user.Users, error)
}

func (m *mockUsersModel) CreateTable() error                                    { return nil }
func (m *mockUsersModel) Insert(ctx context.Context, data []*user.Users) error  { return nil }
func (m *mockUsersModel) InsertOne(ctx context.Context, data *user.Users) error { return nil }
func (m *mockUsersModel) List(ctx context.Context, req user.UsersListReq) ([]user.Users, int64, error) {
	return nil, 0, nil
}
func (m *mockUsersModel) FindOne(ctx context.Context, id string) (*user.Users, error) {
	return nil, nil
}
func (m *mockUsersModel) Update(ctx context.Context, data *user.Users) error { return nil }
func (m *mockUsersModel) Delete(ctx context.Context, id string) error        { return nil }
func (m *mockUsersModel) FindByIds(ctx context.Context, ids []string) ([]user.Users, error) {
	return nil, nil
}
func (m *mockUsersModel) FindByEmail(ctx context.Context, email string) (*user.Users, error) {
	return nil, nil
}
func (m *mockUsersModel) FindByUsername(ctx context.Context, username string) (*user.Users, error) {
	if m.findByUsernameFunc != nil {
		return m.findByUsernameFunc(ctx, username)
	}
	return nil, nil
}
func (m *mockUsersModel) ListByFilter(ctx context.Context, req user.UsersListFilterReq) ([]user.Users, int64, error) {
	return nil, 0, nil
}

func TestUsernameLogin_Success(t *testing.T) {
	password := "correct-password"
	hashed, _ := auth.HashPassword(password)

	mockStore := &mockUsersModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return &user.Users{
				Id:       "user-123",
				Username: "testuser",
				Password: hashed,
				Role:     1,
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

	l := NewUsernameLoginLogic(context.Background(), svcCtx)
	resp, err := l.UsernameLogin(&pb.UsernameLoginReq{
		Username: "testuser",
		Password: password,
	})

	if err != nil {
		t.Fatalf("登录应该成功，但返回错误: %v", err)
	}
	if resp.UserId != "user-123" {
		t.Fatalf("user_id 不匹配: got %s, want user-123", resp.UserId)
	}
	if resp.Token == "" {
		t.Fatal("token 不应为空")
	}
}

func TestUsernameLogin_UserNotFound(t *testing.T) {
	mockStore := &mockUsersModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return nil, nil // 用户不存在
		},
	}

	svcCtx := &svc.ServiceContext{
		Config:    config.Config{JwtAuth: auth.Config{Secret: "test", Expire: 3600}},
		UserStore: mockStore,
	}

	l := NewUsernameLoginLogic(context.Background(), svcCtx)
	_, err := l.UsernameLogin(&pb.UsernameLoginReq{
		Username: "nonexistent",
		Password: "any",
	})

	if err != errors.UserNotFound {
		t.Fatalf("期望 UserNotFound，但得到: %v", err)
	}
}

func TestUsernameLogin_InvalidPassword(t *testing.T) {
	hashed, _ := auth.HashPassword("correct-password")

	mockStore := &mockUsersModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return &user.Users{
				Id:       "user-123",
				Username: "testuser",
				Password: hashed,
				Role:     1,
			}, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config:    config.Config{JwtAuth: auth.Config{Secret: "test", Expire: 3600}},
		UserStore: mockStore,
	}

	l := NewUsernameLoginLogic(context.Background(), svcCtx)
	_, err := l.UsernameLogin(&pb.UsernameLoginReq{
		Username: "testuser",
		Password: "wrong-password",
	})

	if err != errors.InvalidPassword {
		t.Fatalf("期望 InvalidPassword，但得到: %v", err)
	}
}
