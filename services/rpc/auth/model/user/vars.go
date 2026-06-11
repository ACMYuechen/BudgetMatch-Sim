package user

// Role 3位区间编码：百位=层级
// 1xx=用户层  9xx=管理层
const (
	RoleUser       int64 = 100 // 普通用户
	RoleAdmin      int64 = 900 // 管理员
	RoleSuperAdmin int64 = 999 // 超级管理员
)

// Status 用户状态
const (
	StatusNormal   int64 = 1 // 正常
	StatusDisabled int64 = 2 // 禁用
)
