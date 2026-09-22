package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/authclient"
	shared "budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/auth/internal/config"
	"budgetmatch-sim/services/rpc/auth/internal/interceptor"
	authserver "budgetmatch-sim/services/rpc/auth/internal/server/authservice"
	userserver "budgetmatch-sim/services/rpc/auth/internal/server/userservice"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type accountStore struct {
	user.UsersModel
	mu    sync.Mutex
	users map[string]user.Users
}

func (s *accountStore) FindOne(_ context.Context, id string) (*user.Users, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, nil
	}
	return &u, nil
}
func (s *accountStore) FindByUsername(ctx context.Context, name string) (*user.Users, error) {
	return s.FindOne(ctx, name)
}
func (s *accountStore) FindByEmail(ctx context.Context, email string) (*user.Users, error) {
	return s.FindOne(ctx, email)
}
func (s *accountStore) Update(_ context.Context, u *user.Users) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.Id] = *u
	return nil
}
func (s *accountStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, id)
	return nil
}
func (s *accountStore) ListByFilter(context.Context, user.UsersListFilterReq) ([]user.Users, int64, error) {
	return nil, 0, nil
}

const accountTestSecret = "account-test-secret"

func accountContext(t *testing.T, id string, role int) context.Context {
	t.Helper()
	token, err := auth.GenerateToken(id, accountTestSecret, 3600, role)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
}
func accountServer(t *testing.T) (*grpc.ClientConn, *accountStore, *redis.Client) {
	t.Helper()
	password, err := auth.HashPassword("password")
	require.NoError(t, err)
	store := &accountStore{users: map[string]user.Users{}}
	for id, role := range map[string]int{"user": 100, "admin": 2, "super": 1, "other": 100, "disabled": 100} {
		u := user.Users{Id: id, Username: id, Email: id, Password: password, Role: role, Status: user.StatusNormal}
		if id == "disabled" {
			u.Status = user.StatusDisabled
		}
		store.users[id] = u
	}
	redisServer := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = r.Close() })
	service := &svc.ServiceContext{Config: config.Config{JwtAuth: auth.Config{Secret: accountTestSecret, Expire: 3600}}, UserStore: store, Redis: r}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(interceptor.AuthInterceptor(service)))
	pb.RegisterAuthServiceServer(server, authserver.NewAuthServiceServer(service))
	pb.RegisterUserServiceServer(server, userserver.NewUserServiceServer(service))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///auth-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn, store, r
}

func TestUserManagementRPCRejectsOrdinaryUsers(t *testing.T) {
	conn, _, _ := accountServer(t)
	client := pb.NewUserServiceClient(conn)
	for _, ctx := range []context.Context{context.Background(), accountContext(t, "user", 100)} {
		_, err := client.ListUsers(ctx, &pb.ListUsersReq{})
		require.Error(t, err)
		_, err = client.GetUserById(ctx, &pb.GetUserByIdReq{UserId: "other"})
		require.Error(t, err)
		_, err = client.UpdateUserInfo(ctx, &pb.UpdateUserInfoReq{UserId: "user", Role: 1})
		require.Error(t, err)
		_, err = client.DeleteUser(ctx, &pb.DeleteUserReq{UserId: "other"})
		require.Error(t, err)
	}
	self, err := client.GetUserInfo(accountContext(t, "user", 100), &pb.GetUserInfoReq{})
	require.NoError(t, err)
	require.Equal(t, "user", self.User.Id)
	_, err = client.ListUsers(accountContext(t, "admin", 2), &pb.ListUsersReq{})
	require.NoError(t, err)
	_, err = client.GetUserById(accountContext(t, "admin", 2), &pb.GetUserByIdReq{UserId: "other"})
	require.NoError(t, err)
}

func TestOnlySuperAdminCanGrantRolesOrModifyAdmins(t *testing.T) {
	conn, _, _ := accountServer(t)
	client := pb.NewUserServiceClient(conn)
	admin := accountContext(t, "admin", 2)
	for _, req := range []*pb.UpdateUserInfoReq{{UserId: "user", Role: 2}, {UserId: "user", Role: 1}, {UserId: "super", Status: 2}, {UserId: "admin", Email: "takeover"}} {
		_, err := client.UpdateUserInfo(admin, req)
		require.Error(t, err)
	}
	_, err := client.DeleteUser(admin, &pb.DeleteUserReq{UserId: "super"})
	require.Error(t, err)
	_, err = client.UpdateUserInfo(admin, &pb.UpdateUserInfoReq{UserId: "other", Remark: "reviewed"})
	require.NoError(t, err)
	super := accountContext(t, "super", 1)
	_, err = client.UpdateUserInfo(super, &pb.UpdateUserInfoReq{UserId: "other", Role: 200})
	require.Error(t, err)
	_, err = client.UpdateUserInfo(super, &pb.UpdateUserInfoReq{UserId: "other", Status: 99})
	require.Error(t, err)
	_, err = client.UpdateUserInfo(super, &pb.UpdateUserInfoReq{UserId: "other", Role: 2})
	require.NoError(t, err)
	_, err = client.DeleteUser(super, &pb.DeleteUserReq{UserId: "other"})
	require.NoError(t, err)
}

func TestDisabledAccountCannotLoginOrValidate(t *testing.T) {
	conn, _, r := accountServer(t)
	client := pb.NewAuthServiceClient(conn)
	// Valid accounts still succeed through each login mechanism.
	_, loginErr := client.UsernameLogin(context.Background(), &pb.UsernameLoginReq{Username: "user", Password: "password"})
	require.NoError(t, loginErr)
	_, loginErr = client.EmailLogin(context.Background(), &pb.EmailLoginReq{Email: "user", Password: "password"})
	require.NoError(t, loginErr)
	require.NoError(t, r.Set(context.Background(), "email_code:user", "654321", time.Minute).Err())
	_, loginErr = client.LoginByCode(context.Background(), &pb.LoginByCodeReq{Email: "user", Code: "654321"})
	require.NoError(t, loginErr)
	_, err := client.UsernameLogin(context.Background(), &pb.UsernameLoginReq{Username: "disabled", Password: "password"})
	require.Error(t, err)
	_, err = client.EmailLogin(context.Background(), &pb.EmailLoginReq{Email: "disabled", Password: "password"})
	require.Error(t, err)
	// Use the same Redis namespace as the authentication service.
	require.NoError(t, r.Set(context.Background(), "email_code:disabled", "123456", time.Minute).Err())
	_, err = client.LoginByCode(context.Background(), &pb.LoginByCodeReq{Email: "disabled", Code: "123456"})
	require.Error(t, err)
	token, err := auth.GenerateToken("disabled", accountTestSecret, 3600, 100)
	require.NoError(t, err)
	_, err = client.ValidateToken(context.Background(), &pb.ValidateTokenReq{Token: token})
	require.Error(t, err)
	_, err = pb.NewUserServiceClient(conn).GetUserInfo(accountContext(t, "disabled", 100), &pb.GetUserInfoReq{})
	require.Error(t, err)
}

// The real Auth RPC feeds the business interceptor; stale signed JWTs must fail
// even when a caller skips the HTTP gateway entirely.
func TestDirectBusinessRPCChecksCurrentAccount(t *testing.T) {
	conn, store, _ := accountServer(t)
	validator := authclient.Validator(pb.NewAuthServiceClient(conn))
	authenticate := shared.UnaryServerInterceptor(shared.AuthConfig{Secret: accountTestSecret, ValidateUser: validator, AdminMethods: map[string]struct{}{"/test/Admin": {}}})
	token, err := auth.GenerateToken("admin", accountTestSecret, 3600, 2)
	require.NoError(t, err)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	invoke := func() error {
		_, err := authenticate(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test/Admin"}, func(context.Context, any) (any, error) { return nil, nil })
		return err
	}
	require.NoError(t, invoke())
	current, err := store.FindOne(context.Background(), "admin")
	require.NoError(t, err)
	current.Role = 100
	require.NoError(t, store.Update(context.Background(), current))
	require.Error(t, invoke())
	current.Role = 2
	current.Status = 2
	require.NoError(t, store.Update(context.Background(), current))
	require.Error(t, invoke())
	require.NoError(t, store.Delete(context.Background(), "admin"))
	require.Error(t, invoke())
}
