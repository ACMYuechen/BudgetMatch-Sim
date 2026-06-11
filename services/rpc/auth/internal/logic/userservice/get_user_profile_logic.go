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

type GetUserProfileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserProfileLogic {
	return &GetUserProfileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserProfileLogic) GetUserProfile(in *pb.GetUserProfileReq) (*pb.GetUserProfileResp, error) {
	// 强制使用 interceptor 注入的当前用户 ID，忽略请求中的 UserId，防止水平越权
	u, ok := l.ctx.Value(interceptor.ContextKeyUser).(*user.Users)
	if !ok || u == nil {
		l.Logger.Error("user not found in context")
		return nil, errors.ErrUnauthorized
	}
	userId := u.Id

	profile, err := l.svcCtx.UserProfileStore.FindByUserId(l.ctx, userId)
	if err != nil {
		l.Logger.Errorf("failed to find user profile: %v", err)
		return nil, err
	}
	if profile == nil {
		return &pb.GetUserProfileResp{UserId: userId}, nil
	}

	return &pb.GetUserProfileResp{
		UserId:           profile.UserId,
		RealName:         profile.RealName,
		School:           profile.School,
		Major:            profile.Major,
		Grade:            profile.Grade,
		Gender:           int32(profile.Gender),
		ExpectedCity:     profile.ExpectedCity,
		ExpectedPosition: profile.ExpectedPosition,
		SelfIntroduction: profile.SelfIntroduction,
	}, nil
}
