package user

import (
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/auth/pb"
)

func userToInfo(u *pb.UserInfo) types.UserInfo {
	if u == nil {
		return types.UserInfo{}
	}
	return types.UserInfo{
		Id:       u.Id,
		Username: u.Username,
		Email:    u.Email,
		Avatar:   u.Avatar,
		Phone:    u.Phone,
		Role:     int(u.Role),
		Status:   int(u.Status),
		Remark:   u.Remark,
	}
}
