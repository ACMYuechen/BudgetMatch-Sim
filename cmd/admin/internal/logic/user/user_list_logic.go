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

type UserListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserListLogic {
	return &UserListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserListLogic) UserList(req *types.UserListReq) (resp *types.UserListResp, err error) {
	rpcResp, err := l.svcCtx.UserClient.ListUsers(l.ctx, &pb.ListUsersReq{
		Page:     int32(req.Page),
		PageSize: int32(req.PageSize),
		Status:   int32(req.Status),
		Role:     int32(req.Role),
	})
	if err != nil {
		l.Logger.Errorf("failed to list users via auth-rpc: %v", err)
		return nil, errors.ErrDatabase
	}

	items := make([]types.UserItem, 0, len(rpcResp.List))
	for _, u := range rpcResp.List {
		items = append(items, types.UserItem{
			UserId:    u.UserId,
			Username:  u.Username,
			Email:     u.Email,
			Phone:     u.Phone,
			Role:      int64(u.Role),
			Status:    int64(u.Status),
			Avatar:    u.Avatar,
			CreatedAt: u.CreatedAt,
		})
	}

	return &types.UserListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
