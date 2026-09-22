package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserLogic {
	return &DeleteUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 管理后台接口 — 删除用户
func (l *DeleteUserLogic) DeleteUser(in *pb.DeleteUserReq) (*pb.DeleteUserResp, error) {
	actor, err := requireAdmin(l.ctx)
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, errors.Invalid
	}
	if in.UserId == "" {
		return nil, errors.Invalid
	}
	target, err := l.svcCtx.UserStore.FindOne(l.ctx, in.UserId)
	if err != nil {
		return nil, errors.Database
	}
	if target == nil {
		return nil, errors.UserNotFound
	}
	if err := authorizeUserMutation(actor, target, 0); err != nil {
		return nil, err
	}
	if err := l.svcCtx.UserStore.Delete(l.ctx, in.UserId); err != nil {
		l.Logger.Errorf("failed to delete user: %v, error: %v", in.UserId, err)
		return nil, errors.Database
	}

	return &pb.DeleteUserResp{Success: true}, nil
}
