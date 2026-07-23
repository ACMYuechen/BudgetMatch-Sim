// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ActivityCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建活动
func NewActivityCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityCreateLogic {
	return &ActivityCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityCreateLogic) ActivityCreate(req *types.ActivityCreateReq) (resp *types.ActivityCreateResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.CreateActivity(l.ctx, &pb.CreateActivityReq{
		Title:       req.Title,
		Description: req.Description,
		BannerUrl:   req.BannerUrl,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		l.Logger.Errorf("failed to create activity: %v", err)
		return nil, err
	}

	return &types.ActivityCreateResp{
		Id: rpcResp.Id,
	}, nil
}
