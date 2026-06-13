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

type SkuDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 删除SKU
func NewSkuDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SkuDeleteLogic {
	return &SkuDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SkuDeleteLogic) SkuDelete(req *types.SkuDeleteReq) (resp *types.SkuDeleteResp, err error) {
	rpcResp, err := l.svcCtx.SkuClient.DeleteSku(l.ctx, &pb.DeleteSkuReq{
		Id: req.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to delete sku: %v", err)
		return nil, errors.ErrDatabase
	}

	return &types.SkuDeleteResp{
		Success: rpcResp.Success,
	}, nil
}
