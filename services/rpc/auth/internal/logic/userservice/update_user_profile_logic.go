package userservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/internal/interceptor"
	"budgetmatch-sim/services/rpc/auth/internal/svc"
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/model/user_profile"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateUserProfileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateUserProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserProfileLogic {
	return &UpdateUserProfileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateUserProfileLogic) UpdateUserProfile(in *pb.UpdateUserProfileReq) (*pb.UpdateUserProfileResp, error) {
	// 强制使用 interceptor 注入的当前用户 ID，忽略请求中的 UserId，防止水平越权
	u, ok := l.ctx.Value(interceptor.ContextKeyUser).(*user.Users)
	if !ok || u == nil {
		l.Logger.Error("user not found in context")
		return nil, errors.Unauthorized
	}
	userId := u.Id

	err := l.svcCtx.UserProfileStore.Upsert(l.ctx, &user_profile.UserProfiles{
		UserId:           userId,
		RealName:         in.RealName,
		School:           in.School,
		Major:            in.Major,
		Grade:            in.Grade,
		Gender:           int64(in.Gender),
		ExpectedCity:     in.ExpectedCity,
		ExpectedPosition: in.ExpectedPosition,
		SelfIntroduction: in.SelfIntroduction,
	})
	if err != nil {
		l.Logger.Errorf("failed to upsert user profile: %v", err)
		return nil, err
	}

	return &pb.UpdateUserProfileResp{Success: true}, nil
}
