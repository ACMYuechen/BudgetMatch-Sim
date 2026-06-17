// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AcquireTokenLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 获取秒杀令牌
func NewAcquireTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AcquireTokenLogic {
	return &AcquireTokenLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AcquireTokenLogic) AcquireToken(req *types.AcquireTokenReq) (resp *types.AcquireTokenResp, err error) {
	userID := l.ctx.Value("user_id")
	if userID == nil {
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.SeckillClient.AcquireToken(l.ctx, &pb.AcquireTokenReq{
		ActivityId: req.ActivityId,
		SkuId:      req.SkuId,
		UserId:     userID.(string),
	})
	if err != nil {
		l.Logger.Errorf("failed to acquire token: %v", err)
		return nil, errors.SeckillTokenInvalid
	}

	return &types.AcquireTokenResp{
		Token: rpcResp.Token,
	}, nil
}
