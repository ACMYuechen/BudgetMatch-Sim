package authservicelogic

import (
	"context"
	"regexp"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

type EmailRegisterLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewEmailRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EmailRegisterLogic {
	return &EmailRegisterLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *EmailRegisterLogic) EmailRegister(in *pb.EmailRegisterReq) (*pb.RegisterResp, error) {
	// 校验邮箱格式
	if !emailRegex.MatchString(in.Email) {
		return nil, errors.InvalidEmail
	}

	// 用户名是否存在
	existingUser, err := l.svcCtx.UserStore.FindByUsername(l.ctx, in.Username)
	if err != nil {
		l.Logger.Errorf("failed to check existing user by username: %v, error: %v", in.Username, err)
		return nil, errors.Database
	}
	if existingUser != nil {
		return nil, errors.UserExists
	}

	// 邮箱是否已注册
	existingUserByEmail, err := l.svcCtx.UserStore.FindByEmail(l.ctx, in.Email)
	if err != nil {
		l.Logger.Errorf("failed to check existing user by email: %v, error: %v", in.Email, err)
		return nil, errors.Database
	}
	if existingUserByEmail != nil {
		return nil, errors.UserExists
	}

	// 验证码校验
	codeKey := CodeRedisKeyPrefix + in.Email
	storedCode, err := l.svcCtx.Redis.Get(l.ctx, codeKey).Result()
	if err == redis.Nil {
		return nil, errors.CodeExpired
	}
	if err != nil {
		l.Logger.Errorf("failed to get code from Redis for email: %v, error: %v", in.Email, err)
		return nil, errors.Database
	}
	if storedCode != in.Code {
		return nil, errors.CodeInvalid
	}

	// 删除验证码，防止重复使用
	err = l.svcCtx.Redis.Del(l.ctx, codeKey).Err()
	if err != nil {
		l.Logger.Errorf("failed to delete code from Redis for email: %v, error: %v", in.Email, err)
		return nil, errors.Database
	}

	// 哈希密码
	hashPassword, err := auth.HashPassword(in.Password)
	if err != nil {
		l.Logger.Errorf("failed to hash password, error: %v", err)
		return nil, errors.Internal
	}

	// 创建新用户
	err = l.svcCtx.UserStore.InsertOne(l.ctx, &user.Users{
		Id:        user.NewUserId(),
		Username:  in.Username,
		Email:     in.Email,
		Password:  hashPassword,
		Role:      role.RoleUser,
		Status:    user.StatusNormal,
		UpdatedAt: time.Now(),
		CreatedAt: time.Now(),
	})
	if err != nil {
		l.Logger.Errorf("failed to create user with email: %v, error: %v", in.Email, err)
		return nil, errors.Database
	}

	return &pb.RegisterResp{
		Success: true,
	}, nil
}
