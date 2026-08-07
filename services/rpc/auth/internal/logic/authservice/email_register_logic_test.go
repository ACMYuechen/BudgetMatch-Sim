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

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupRegisterTestRedis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	s := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: s.Addr()})
	return s, r
}

// mockRegisterUserModel 用于注册测试
type mockRegisterUserModel struct {
	findByUsernameFunc func(ctx context.Context, username string) (*user.Users, error)
	findByEmailFunc    func(ctx context.Context, email string) (*user.Users, error)
	insertOneFunc      func(ctx context.Context, data *user.Users) error
}

func (m *mockRegisterUserModel) CreateTable() error                                   { return nil }
func (m *mockRegisterUserModel) Insert(ctx context.Context, data []*user.Users) error { return nil }
func (m *mockRegisterUserModel) InsertOne(ctx context.Context, data *user.Users) error {
	if m.insertOneFunc != nil {
		return m.insertOneFunc(ctx, data)
	}
	return nil
}
func (m *mockRegisterUserModel) List(ctx context.Context, req user.UsersListReq) ([]user.Users, int64, error) {
	return nil, 0, nil
}
func (m *mockRegisterUserModel) FindOne(ctx context.Context, id string) (*user.Users, error) {
	return nil, nil
}
func (m *mockRegisterUserModel) Update(ctx context.Context, data *user.Users) error { return nil }
func (m *mockRegisterUserModel) Delete(ctx context.Context, id string) error        { return nil }
func (m *mockRegisterUserModel) FindByIds(ctx context.Context, ids []string) ([]user.Users, error) {
	return nil, nil
}
func (m *mockRegisterUserModel) FindByEmail(ctx context.Context, email string) (*user.Users, error) {
	if m.findByEmailFunc != nil {
		return m.findByEmailFunc(ctx, email)
	}
	return nil, nil
}
func (m *mockRegisterUserModel) FindByUsername(ctx context.Context, username string) (*user.Users, error) {
	if m.findByUsernameFunc != nil {
		return m.findByUsernameFunc(ctx, username)
	}
	return nil, nil
}
func (m *mockRegisterUserModel) ListByFilter(ctx context.Context, req user.UsersListFilterReq) ([]user.Users, int64, error) {
	return nil, 0, nil
}

// TestEmailRegister_Success 测试注册成功场景
// 验证提供正确的邮箱、密码、用户名和验证码后，能够成功注册
func TestEmailRegister_Success(t *testing.T) {
	_, redisClient := setupRegisterTestRedis(t)

	mockStore := &mockRegisterUserModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return nil, nil
		},
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return nil, nil
		},
	}

	// 先存入正确的验证码
	codeKey := CodeRedisKeyPrefix + "user@example.com"
	redisClient.Set(context.Background(), codeKey, "123456", 300*1000*1000*1000).Result()

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{
				Secret: "test-secret",
				Expire: 3600,
			},
		},
		UserStore: mockStore,
		Redis:     redisClient,
	}

	l := NewEmailRegisterLogic(context.Background(), svcCtx)
	resp, err := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "user@example.com",
		Password: "secure-password",
		Username: "newuser",
		Code:     "123456",
	})

	if err != nil {
		t.Fatalf("注册应该成功，但返回错误: %v", err)
	}
	if !resp.Success {
		t.Fatal("注册响应的 Success 应该为 true")
	}
}

// TestEmailRegister_InvalidEmailFormat 测试邮箱格式错误场景
// 验证使用无效邮箱格式注册时，返回 InvalidEmail 错误
func TestEmailRegister_InvalidEmailFormat(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
	}

	l := NewEmailRegisterLogic(context.Background(), svcCtx)
	_, err := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "invalid-email",
		Password: "secure-password",
		Username: "newuser",
		Code:     "123456",
	})

	if err != errors.InvalidEmail {
		t.Fatalf("期望 InvalidEmail，但得到: %v", err)
	}
}

// TestEmailRegister_UsernameAlreadyExists 测试用户名已存在场景
// 验证使用已被注册的用户名注册时，返回 UserExists 错误
func TestEmailRegister_UsernameAlreadyExists(t *testing.T) {
	mockStore := &mockRegisterUserModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return &user.Users{
				Id:       "existing-user",
				Username: username,
				Email:    "old@example.com",
				Password: "hashed",
				Role:     role.RoleUser,
			}, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
		UserStore: mockStore,
	}

	l := NewEmailRegisterLogic(context.Background(), svcCtx)
	_, err := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "new@example.com",
		Password: "secure-password",
		Username: "existing-user",
		Code:     "123456",
	})

	if err != errors.UserExists {
		t.Fatalf("期望 UserExists，但得到: %v", err)
	}
}

// TestEmailRegister_EmailAlreadyExists 测试邮箱已存在场景
// 验证使用已被注册的邮箱注册时，返回 UserExists 错误
func TestEmailRegister_EmailAlreadyExists(t *testing.T) {
	mockStore := &mockRegisterUserModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return nil, nil
		},
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return &user.Users{
				Id:       "existing-user",
				Username: "olduser",
				Email:    email,
				Password: "hashed",
				Role:     role.RoleUser,
			}, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
		UserStore: mockStore,
	}

	l := NewEmailRegisterLogic(context.Background(), svcCtx)
	_, err := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "existing@example.com",
		Password: "secure-password",
		Username: "newuser",
		Code:     "123456",
	})

	if err != errors.UserExists {
		t.Fatalf("期望 UserExists，但得到: %v", err)
	}
}

// TestEmailRegister_CodeExpired 测试验证码过期场景
// 验证使用已过期或未发送的验证码注册时，返回 CodeExpired 错误
func TestEmailRegister_CodeExpired(t *testing.T) {
	_, redisClient := setupRegisterTestRedis(t)

	mockStore := &mockRegisterUserModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return nil, nil
		},
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return nil, nil
		},
	}

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
		UserStore: mockStore,
		Redis:     redisClient,
	}

	l := NewEmailRegisterLogic(context.Background(), svcCtx)
	// 验证码不存在（已过期或未发送）
	_, err := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "user@example.com",
		Password: "secure-password",
		Username: "newuser",
		Code:     "123456",
	})

	if err != errors.CodeExpired {
		t.Fatalf("期望 CodeExpired，但得到: %v", err)
	}
}

// TestEmailRegister_CodeInvalid 测试验证码错误场景
// 验证使用正确邮箱但错误验证码注册时，返回 CodeInvalid 错误
func TestEmailRegister_CodeInvalid(t *testing.T) {
	_, redisClient := setupRegisterTestRedis(t)

	mockStore := &mockRegisterUserModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return nil, nil
		},
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return nil, nil
		},
	}

	// 存入不同的验证码
	codeKey := CodeRedisKeyPrefix + "user@example.com"
	redisClient.Set(context.Background(), codeKey, "654321", 300*1000*1000*1000).Result()

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
		UserStore: mockStore,
		Redis:     redisClient,
	}

	l := NewEmailRegisterLogic(context.Background(), svcCtx)
	// 提交错误的验证码
	_, err := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "user@example.com",
		Password: "secure-password",
		Username: "newuser",
		Code:     "123456",
	})

	if err != errors.CodeInvalid {
		t.Fatalf("期望 CodeInvalid，但得到: %v", err)
	}
}

// TestEmailRegister_CodeConsumed 测试验证码使用后立即删除场景
// 验证验证码使用后立即删除，防止重复使用同一验证码进行注册
func TestEmailRegister_CodeConsumed(t *testing.T) {
	// 验证验证码使用后立即删除，防止重复使用
	_, redisClient := setupRegisterTestRedis(t)

	callCount := 0
	mockStore := &mockRegisterUserModel{
		findByUsernameFunc: func(ctx context.Context, username string) (*user.Users, error) {
			return nil, nil
		},
		findByEmailFunc: func(ctx context.Context, email string) (*user.Users, error) {
			return nil, nil
		},
		insertOneFunc: func(ctx context.Context, data *user.Users) error {
			callCount++
			return nil
		},
	}

	// 存入验证码
	codeKey := CodeRedisKeyPrefix + "user@example.com"
	redisClient.Set(context.Background(), codeKey, "123456", 300*1000*1000*1000).Result()

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			JwtAuth: auth.Config{Secret: "test-secret", Expire: 3600},
		},
		UserStore: mockStore,
		Redis:     redisClient,
	}

	// 第一次注册成功
	l := NewEmailRegisterLogic(context.Background(), svcCtx)
	resp, err := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "user@example.com",
		Password: "secure-password",
		Username: "newuser",
		Code:     "123456",
	})
	if err != nil {
		t.Fatalf("第一次注册应该成功，但返回错误: %v", err)
	}
	if !resp.Success {
		t.Fatal("第一次注册响应的 Success 应该为 true")
	}
	if callCount != 1 {
		t.Fatalf("InsertOne 应该被调用 1 次，但实际调用了 %d 次", callCount)
	}

	// 第二次使用同一验证码应失败（已被删除）
	resp2, err2 := l.EmailRegister(&pb.EmailRegisterReq{
		Email:    "user2@example.com",
		Password: "another-password",
		Username: "newuser2",
		Code:     "123456",
	})
	if err2 != errors.CodeExpired {
		t.Fatalf("验证码已使用后应返回 CodeExpired，但得到: %v", err2)
	}
	if resp2 != nil && resp2.Success {
		t.Fatal("不应允许重复使用验证码")
	}
}
