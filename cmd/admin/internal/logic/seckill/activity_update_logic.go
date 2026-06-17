// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ActivityUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新活动
func NewActivityUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityUpdateLogic {
	return &ActivityUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityUpdateLogic) ActivityUpdate(req *types.ActivityUpdateReq) (resp *types.ActivityUpdateResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.UpdateActivity(l.ctx, &pb.UpdateActivityReq{
		Id:          req.Id,
		Title:       req.Title,
		Description: req.Description,
		BannerUrl:   req.BannerUrl,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		l.Logger.Errorf("failed to update activity: %v", err)
		return nil, errors.Database
	}

	return &types.ActivityUpdateResp{
		Success: rpcResp.Success,
	}, nil
}
