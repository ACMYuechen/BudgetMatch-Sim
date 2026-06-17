package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/interceptor"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
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
	// 强制使用 interceptor 注入的当前用户，忽略请求中的 UserId，防止水平越权
	u, ok := l.ctx.Value(interceptor.ContextKeyUser).(*user.Users)
	if !ok || u == nil {
		l.Logger.Error("user not found in context")
		return nil, errors.Unauthorized
	}

	return &pb.GetUserInfoResp{
		UserId:   u.Id,
		Username: u.Username,
		Email:    u.Email,
		Avatar:   u.Avatar,
		Phone:    u.Phone,
	}, nil
}
