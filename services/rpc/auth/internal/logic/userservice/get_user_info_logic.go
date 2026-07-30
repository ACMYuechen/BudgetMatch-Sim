package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserInfoLogic {
	return &GetUserInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserInfoLogic) GetUserInfo(in *pb.GetUserInfoReq) (*pb.GetUserInfoResp, error) {
	// 强制使用认证拦截器注入的用户 ID，忽略请求中的 UserId，防止水平越权
	userID := request.TryUserID(l.ctx)
	if userID == "" {
		l.Logger.Error("user id not found in request context")
		return nil, errors.Unauthorized
	}

	u, err := l.svcCtx.UserStore.FindOne(l.ctx, userID)
	if err != nil {
		l.Logger.Errorf("failed to find current user: user_id=%s, error=%v", userID, err)
		return nil, errors.Database
	}
	if u == nil {
		l.Logger.Errorf("current user not found: user_id=%s", userID)
		return nil, errors.UserNotFound
	}

	return &pb.GetUserInfoResp{
		User: userToInfo(u),
	}, nil
}
