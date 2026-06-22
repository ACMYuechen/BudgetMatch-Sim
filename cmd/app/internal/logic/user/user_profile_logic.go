// Code scaffolded by goctl. No recover, Safe to edit.

package user

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/auth/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserProfileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 获取当前用户资料
func NewUserProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserProfileLogic {
	return &UserProfileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserProfileLogic) UserProfile(req *types.UserProfileReq) (resp *types.UserProfileResp, err error) {
	userID := l.ctx.Value("user_id")
	if userID == nil {
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.UserClient.GetUserProfile(l.ctx, &pb.GetUserProfileReq{
		UserId: userID.(string),
	})
	if err != nil {
		l.Logger.Errorf("failed to get user profile: %v", err)
		return nil, errors.Internal
	}

	return &types.UserProfileResp{
		UserId:           rpcResp.UserId,
		RealName:         rpcResp.RealName,
		School:           rpcResp.School,
		Major:            rpcResp.Major,
		Grade:            rpcResp.Grade,
		Gender:           rpcResp.Gender,
		ExpectedCity:     rpcResp.ExpectedCity,
		ExpectedPosition: rpcResp.ExpectedPosition,
		SelfIntroduction: rpcResp.SelfIntroduction,
	}, nil
}
