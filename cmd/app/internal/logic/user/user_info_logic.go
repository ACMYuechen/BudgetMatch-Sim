// Code scaffolded by goctl. No recover, Safe to edit.

package user

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 获取当前用户信息
func NewUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserInfoLogic {
	return &UserInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserInfoLogic) UserInfo(req *types.UserInfoReq) (resp *types.UserInfoResp, err error) {
	userID := l.ctx.Value("user_id")
	if userID == nil {
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.UserClient.GetUserInfo(l.ctx, &pb.GetUserInfoReq{
		UserId: userID.(string),
	})
	if err != nil {
		l.Logger.Errorf("failed to get user info: %v", err)
		return nil, errors.Internal
	}

	return &types.UserInfoResp{
		UserId:   rpcResp.UserId,
		Username: rpcResp.Username,
		Email:    rpcResp.Email,
		Avatar:   rpcResp.Avatar,
		Phone:    rpcResp.Phone,
	}, nil
}
