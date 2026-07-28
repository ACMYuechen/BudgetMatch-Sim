package authservicelogic

import (
	"budgetmatch-sim/services/rpc/auth/model/user"
	"budgetmatch-sim/services/rpc/auth/pb"
)

const (
	// 验证码过期时间，单位为秒
	CodeExpireTime = 300
	// Redis 中验证码的 key 前缀
	CodeRedisKeyPrefix = "email_code:"
	// 限流时间窗口，单位为秒
	RateLimitWindow = 60
	// 限流 Redis key 前缀
	RateLimitRedisKeyPrefix = "email_code_rate_limit:"
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
