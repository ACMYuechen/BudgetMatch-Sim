package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListUsersLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListUsersLogic {
	return &ListUsersLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 管理后台接口 — 用户列表
func (l *ListUsersLogic) ListUsers(in *pb.ListUsersReq) (*pb.ListUsersResp, error) {
	req := user.UsersListFilterReq{
		Page:   int(in.Page),
		Size:   int(in.PageSize),
		Status: int64(in.Status),
		Role:   int64(in.Role),
	}

	list, total, err := l.svcCtx.UserStore.ListByFilter(l.ctx, req)
	if err != nil {
		l.Logger.Errorf("failed to list users: %v", err)
		return nil, errors.Database
	}

	items := make([]*pb.UserInfo, 0, len(list))
	for _, u := range list {
		items = append(items, userToInfo(&u))
	}

	return &pb.ListUsersResp{Total: int32(total), List: items}, nil
}
