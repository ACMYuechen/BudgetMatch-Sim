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

type UpdateUserProfileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新当前用户资料
func NewUpdateUserProfileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserProfileLogic {
	return &UpdateUserProfileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserProfileLogic) UpdateUserProfile(req *types.UpdateUserProfileReq) (resp *types.UpdateUserProfileResp, err error) {
	userID := l.ctx.Value("user_id")
	if userID == nil {
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.UserClient.UpdateUserProfile(l.ctx, &pb.UpdateUserProfileReq{
		UserId:           userID.(string),
		RealName:         req.RealName,
		School:           req.School,
		Major:            req.Major,
		Grade:            req.Grade,
		Gender:           req.Gender,
		ExpectedCity:     req.ExpectedCity,
		ExpectedPosition: req.ExpectedPosition,
		SelfIntroduction: req.SelfIntroduction,
	})
	if err != nil {
		l.Logger.Errorf("failed to update user profile: %v", err)
		return nil, errors.Internal
	}

	return &types.UpdateUserProfileResp{
		Success: rpcResp.Success,
	}, nil
}
