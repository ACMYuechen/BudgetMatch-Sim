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

	items := make([]*pb.UserListItem, 0, len(list))
	for _, u := range list {
		items = append(items, &pb.UserListItem{
			UserId:    u.Id,
			Username:  u.Username,
			Email:     u.Email,
			Phone:     u.Phone,
			Role:      int32(u.Role),
			Status:    int32(u.Status),
			Avatar:    u.Avatar,
			CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	return &pb.ListUsersResp{
		List:     items,
		Total:    total,
		Page:     in.Page,
		PageSize: in.PageSize,
	}, nil
}
