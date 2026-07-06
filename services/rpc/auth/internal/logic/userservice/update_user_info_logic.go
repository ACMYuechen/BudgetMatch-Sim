package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateUserInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserInfoLogic {
	return &UpdateUserInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 管理后台接口 — 更新用户信息（按 user_id 定位，仅覆盖请求中提供的字段）
func (l *UpdateUserInfoLogic) UpdateUserInfo(in *pb.UpdateUserInfoReq) (*pb.UpdateUserInfoResp, error) {
	if in.UserId == "" {
		return nil, errors.Invalid
	}

	u, err := l.svcCtx.UserStore.FindOne(l.ctx, in.UserId)
	if err != nil {
		l.Logger.Errorf("failed to find user: %v, error: %v", in.UserId, err)
		return nil, errors.Database
	}
	if u == nil {
		return nil, errors.UserNotFound
	}

	// 仅覆盖请求中提供（非零）的字段
	if in.Username != "" {
		u.Username = in.Username
	}
	if in.Email != "" {
		u.Email = in.Email
	}
	if in.Avatar != "" {
		u.Avatar = in.Avatar
	}
	if in.Phone != "" {
		u.Phone = in.Phone
	}
	if in.Role != 0 {
		u.Role = int(in.Role)
	}
	if in.Status != 0 {
		u.Status = int(in.Status)
	}
	if in.Remark != "" {
		u.Remark = in.Remark
	}

	if err := l.svcCtx.UserStore.Update(l.ctx, u); err != nil {
		l.Logger.Errorf("failed to update user info: %v, error: %v", in.UserId, err)
		return nil, errors.Database
	}

	return &pb.UpdateUserInfoResp{User: userToInfo(u)}, nil
}
