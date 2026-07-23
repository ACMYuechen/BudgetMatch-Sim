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

type GetUserByIdLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 按 ID 获取用户
func NewGetUserByIdLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserByIdLogic {
	return &GetUserByIdLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserByIdLogic) GetUserById(req *types.GetUserByIdReq) (resp *types.GetUserByIdResp, err error) {
	rpcResp, err := l.svcCtx.UserClient.GetUserById(l.ctx, &pb.GetUserByIdReq{UserId: req.UserId})
	if err != nil {
		l.Logger.Errorf("failed to get user by id via auth-rpc: %v, error: %v", req.UserId, err)
		return nil, errors.Database
	}
	u := rpcResp.User
	if u == nil {
		l.Logger.Errorf("return error: %v", errors.UserNotFound)
		return nil, errors.UserNotFound
	}

	return &types.GetUserByIdResp{User: userToInfo(u)}, nil
}
