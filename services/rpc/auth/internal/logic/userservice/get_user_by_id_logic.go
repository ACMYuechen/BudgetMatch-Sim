package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserByIdLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserByIdLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserByIdLogic {
	return &GetUserByIdLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 管理后台接口 — 按 user_id 查询任意用户
func (l *GetUserByIdLogic) GetUserById(in *pb.GetUserByIdReq) (*pb.GetUserByIdResp, error) {
	if in.UserId == "" {
		l.Logger.Errorf("return error: %v", errors.Invalid)
		return nil, errors.Invalid
	}

	u, err := l.svcCtx.UserStore.FindOne(l.ctx, in.UserId)
	if err != nil {
		l.Logger.Errorf("failed to find user: %v, error: %v", in.UserId, err)
		return nil, errors.Database
	}
	if u == nil {
		l.Logger.Errorf("return error: %v", errors.UserNotFound)
		return nil, errors.UserNotFound
	}

	return &pb.GetUserByIdResp{
		User: userToInfo(u),
	}, nil
}
