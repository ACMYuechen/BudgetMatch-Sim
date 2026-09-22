package authservicelogic

import (
	"context"

	"budgetmatch-sim/infra/auth"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/role"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"

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
	if in == nil {
		return nil, errors.InvalidToken
	}
	// 获取用户 ID
	userId, err := auth.GetUserIdFromToken(in.Token, l.svcCtx.Config.JwtAuth.Secret)
	if err != nil {
		l.Logger.Error("invalid token")
		return nil, errors.InvalidToken
	}

	// 查库获取完整用户信息
	u, err := l.svcCtx.UserStore.FindOne(l.ctx, userId)
	if err != nil {
		l.Logger.Errorf("failed to find user: %v, error: %v", userId, err)
		return nil, errors.Database
	}
	if u == nil {
		l.Logger.Errorf("user not found: %v", userId)
		return nil, errors.UserNotFound
	}

	if u.Status != user.StatusNormal || !role.IsGlobalUserRole(int64(u.Role)) {
		return nil, errors.Unauthorized
	}
	tokenRole, err := auth.GetUserRoleFromToken(in.Token, l.svcCtx.Config.JwtAuth.Secret)
	if err != nil || tokenRole != u.Role {
		return nil, errors.InvalidToken
	}

	return &pb.ValidateTokenResp{
		User: userToInfo(u),
	}, nil
}
