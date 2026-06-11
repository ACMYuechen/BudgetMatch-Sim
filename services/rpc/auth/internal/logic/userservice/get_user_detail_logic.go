package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserDetailLogic {
	return &GetUserDetailLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 管理后台接口 — 用户详情
func (l *GetUserDetailLogic) GetUserDetail(in *pb.GetUserDetailReq) (*pb.GetUserDetailResp, error) {
	u, err := l.svcCtx.UserStore.FindOne(l.ctx, in.UserId)
	if err != nil {
		l.Logger.Errorf("failed to find user: %v, error: %v", in.UserId, err)
		return nil, errors.ErrDatabase
	}
	if u == nil {
		return nil, errors.ErrUserNotFound
	}

	return &pb.GetUserDetailResp{
		UserId:    u.Id,
		Username:  u.Username,
		Email:     u.Email,
		Phone:     u.Phone,
		Role:      int32(u.Role),
		Status:    int32(u.Status),
		Avatar:    u.Avatar,
		CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt: u.UpdatedAt.Format("2006-01-02 15:04:05"),
	}, nil
}
