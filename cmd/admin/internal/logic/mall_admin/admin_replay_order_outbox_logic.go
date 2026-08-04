// Code scaffolded by goctl. No recover, Safe to edit.

package mall_admin

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdminReplayOrderOutboxLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 重放订单Outbox死信
func NewAdminReplayOrderOutboxLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminReplayOrderOutboxLogic {
	return &AdminReplayOrderOutboxLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminReplayOrderOutboxLogic) AdminReplayOrderOutbox(req *types.AdminReplayOrderOutboxReq) (resp *types.AdminReplayOrderOutboxResp, err error) {
	rpcResp, err := l.svcCtx.MallOrderClient.ReplayOrderOutbox(l.ctx, &pb.ReplayOrderOutboxReq{Id: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to replay order outbox event: event_id=%s error=%v", req.Id, err)
		return nil, err
	}
	return &types.AdminReplayOrderOutboxResp{Success: rpcResp.Success}, nil
}
