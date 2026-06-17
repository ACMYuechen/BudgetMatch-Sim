package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateUserStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateUserStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserStatusLogic {
	return &UpdateUserStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 管理后台接口 — 更新用户状态
func (l *UpdateUserStatusLogic) UpdateUserStatus(in *pb.UpdateUserStatusReq) (*pb.UpdateUserStatusResp, error) {
	u, err := l.svcCtx.UserStore.FindOne(l.ctx, in.UserId)
	if err != nil {
		l.Logger.Errorf("failed to find user: %v, error: %v", in.UserId, err)
		return nil, errors.Database
	}
	if u == nil {
		return nil, errors.UserNotFound
	}

	u.Status = int64(in.Status)
	if err := l.svcCtx.UserStore.Update(l.ctx, u); err != nil {
		l.Logger.Errorf("failed to update user status: %v, error: %v", in.UserId, err)
		return nil, errors.Database
	}

	return &pb.UpdateUserStatusResp{Success: true}, nil
}
