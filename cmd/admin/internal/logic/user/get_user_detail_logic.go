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

type GetUserDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUserDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserDetailLogic {
	return &GetUserDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserDetailLogic) GetUserDetail(req *types.GetUserDetailReq) (resp *types.GetUserDetailResp, err error) {
	rpcResp, err := l.svcCtx.UserClient.GetUserDetail(l.ctx, &pb.GetUserDetailReq{UserId: req.UserId})
	if err != nil {
		l.Logger.Errorf("failed to get user detail via auth-rpc: %v, error: %v", req.UserId, err)
		return nil, errors.ErrDatabase
	}

	return &types.GetUserDetailResp{
		UserId:    rpcResp.UserId,
		Username:  rpcResp.Username,
		Email:     rpcResp.Email,
		Phone:     rpcResp.Phone,
		Role:      int64(rpcResp.Role),
		Status:    int64(rpcResp.Status),
		Avatar:    rpcResp.Avatar,
		CreatedAt: rpcResp.CreatedAt,
		UpdatedAt: rpcResp.UpdatedAt,
	}, nil
}
