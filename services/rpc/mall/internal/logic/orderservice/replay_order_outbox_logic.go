package orderservicelogic

import (
	"context"
	"time"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/outbox"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReplayOrderOutboxLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReplayOrderOutboxLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReplayOrderOutboxLogic {
	return &ReplayOrderOutboxLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ReplayOrderOutboxLogic) ReplayOrderOutbox(in *pb.ReplayOrderOutboxReq) (*pb.ReplayOrderOutboxResp, error) {
	if in == nil || in.Id == "" {
		return nil, errors.Invalid
	}
	ok, err := l.svcCtx.OrderOutboxStore.ReplayDead(l.ctx, in.Id, time.Now())
	if err != nil {
		outbox.RecordReplay("failed")
		l.Logger.Errorf("failed to replay order outbox event: event_id=%s error=%v", in.Id, err)
		return nil, errors.Database
	}
	if !ok {
		event, findErr := l.svcCtx.OrderOutboxStore.FindOne(l.ctx, in.Id)
		if findErr != nil {
			outbox.RecordReplay("failed")
			return nil, errors.Database
		}
		outbox.RecordReplay("rejected")
		if event == nil {
			return nil, errors.NotFound
		}
		return nil, errors.Conflict
	}
	outbox.RecordReplay("success")
	l.Logger.Infof("order outbox dead letter replayed: event_id=%s", in.Id)
	return &pb.ReplayOrderOutboxResp{Success: true}, nil
}
