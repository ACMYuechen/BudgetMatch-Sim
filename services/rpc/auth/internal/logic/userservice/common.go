package userservicelogic

import (
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"
)

func userToInfo(u *user.Users) *pb.UserInfo {
	if u == nil {
		return nil
	}
	return &pb.UserInfo{
		Id:       u.Id,
		Username: u.Username,
		Email:    u.Email,
		Avatar:   u.Avatar,
		Phone:    u.Phone,
		Role:     int32(u.Role),
		Status:   int32(u.Status),
		Remark:   u.Remark,
	}
}
