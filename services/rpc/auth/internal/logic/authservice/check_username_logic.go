package authservicelogic

import (
	"context"

	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CheckUsernameLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCheckUsernameLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckUsernameLogic {
	return &CheckUsernameLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CheckUsernameLogic) CheckUsername(in *pb.CheckUsernameReq) (*pb.CheckUsernameResp, error) {
	existingUser, err := l.svcCtx.UserStore.FindByUsername(l.ctx, in.Username)
	if err != nil {
		l.Logger.Errorf("failed to check username existence: %v, error: %v", in.Username, err)
		return &pb.CheckUsernameResp{Exists: false}, nil
	}
	return &pb.CheckUsernameResp{Exists: existingUser != nil}, nil
}
