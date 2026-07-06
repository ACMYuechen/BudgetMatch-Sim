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

type GetUserInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 获取当前用户信息
func NewGetUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserInfoLogic {
	return &GetUserInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserInfoLogic) GetUserInfo(req *types.GetUserInfoReq) (resp *types.GetUserInfoResp, err error) {
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
	if rpcResp.User == nil {
		return nil, errors.Internal
	}

	u := rpcResp.User
	return &types.GetUserInfoResp{
		User: types.UserInfo{
			Id:       u.Id,
			Username: u.Username,
			Avatar:   u.Avatar,
			Phone:    u.Phone,
			Email:    u.Email,
			Role:     int(u.Role),
			Status:   int(u.Status),
			Remark:   u.Remark,
		},
	}, nil
}
