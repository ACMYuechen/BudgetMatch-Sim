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

type ListUsersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 用户列表
func NewListUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListUsersLogic {
	return &ListUsersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListUsersLogic) ListUsers(req *types.ListUsersReq) (resp *types.ListUsersResp, err error) {
	rpcResp, err := l.svcCtx.UserClient.ListUsers(l.ctx, &pb.ListUsersReq{
		Page:     int32(req.Page),
		PageSize: int32(req.PageSize),
		Status:   int32(req.Status),
		Role:     int32(req.Role),
	})
	if err != nil {
		l.Logger.Errorf("failed to list users via auth-rpc: %v", err)
		return nil, errors.Database
	}

	list := make([]types.UserInfo, 0, len(rpcResp.List))
	for _, u := range rpcResp.List {
		list = append(list, userToInfo(u))
	}

	return &types.ListUsersResp{Total: int(rpcResp.Total), List: list}, nil
}
