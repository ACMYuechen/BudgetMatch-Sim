package authservicelogic

import (
	"context"
	"time"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

type ValidateTokenLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewValidateTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ValidateTokenLogic {
	return &ValidateTokenLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ValidateTokenLogic) ValidateToken(in *pb.ValidateTokenReq) (*pb.ValidateTokenResp, error) {
	// 验证 token
	token, err := auth.ValidateToken(in.Token, l.svcCtx.Config.JwtAuth.Secret)
	if err != nil {
		l.Logger.Errorf("failed to validate token: %v, error: %v", in.Token, err)
		return nil, errors.ErrInvalidToken
	}

	// 显式检查过期时间
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				l.Logger.Errorf("token expired: %v", in.Token)
				return nil, errors.ErrInvalidToken
			}
		}
	}

	// 获取用户 ID
	userId, err := auth.GetUserIdFromToken(in.Token, l.svcCtx.Config.JwtAuth.Secret)
	if err != nil {
		l.Logger.Errorf("invalid token, user ID not found: %v", in.Token)
		return nil, errors.ErrInvalidToken
	}

	// 查库获取完整用户信息
	u, err := l.svcCtx.UserStore.FindOne(l.ctx, userId)
	if err != nil {
		l.Logger.Errorf("failed to find user: %v, error: %v", userId, err)
		return nil, errors.ErrDatabase
	}
	if u == nil {
		l.Logger.Errorf("user not found: %v", userId)
		return nil, errors.ErrUserNotFound
	}

	return &pb.ValidateTokenResp{
		User: &pb.User{
			Id:       u.Id,
			Username: u.Username,
			Email:    u.Email,
			Avatar:   u.Avatar,
			Phone:    u.Phone,
			Role:     int32(u.Role),
			Status:   int32(u.Status),
		},
	}, nil
}
