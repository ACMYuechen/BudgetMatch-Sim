// Code scaffolded by goctl. Safe to edit.

package user

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateUserStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateUserStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserStatusLogic {
	return &UpdateUserStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserStatusLogic) UpdateUserStatus(req *types.UpdateUserStatusReq) (resp *types.UpdateUserStatusResp, err error) {
	_, err = l.svcCtx.UserClient.UpdateUserStatus(l.ctx, &pb.UpdateUserStatusReq{
		UserId: req.UserId,
		Status: int32(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to update user status via auth-rpc: %v, error: %v", req.UserId, err)
		return nil, errors.ErrDatabase
	}

	return &types.UpdateUserStatusResp{Success: true}, nil
}
