// Code scaffolded by goctl. No recover, Safe to edit.

package user

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateUserInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新用户信息
func NewUpdateUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserInfoLogic {
	return &UpdateUserInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserInfoLogic) UpdateUserInfo(req *types.UpdateUserInfoReq) (resp *types.UpdateUserInfoResp, err error) {
	rpcResp, err := l.svcCtx.UserClient.UpdateUserInfo(l.ctx, &pb.UpdateUserInfoReq{
		UserId:   req.UserId,
		Username: req.Username,
		Email:    req.Email,
		Avatar:   req.Avatar,
		Phone:    req.Phone,
		Role:     int32(req.Role),
		Status:   int32(req.Status),
		Remark:   req.Remark,
	})
	if err != nil {
		l.Logger.Errorf("failed to update user info via auth-rpc: %v, error: %v", req.UserId, err)
		return nil, err
	}
	u := rpcResp.User
	if u == nil {
		l.Logger.Errorf("return error: %v", errors.UserNotFound)
		return nil, errors.UserNotFound
	}

	return &types.UpdateUserInfoResp{User: userToInfo(u)}, nil
}
