package role

// 全局管理身份定义 1 ~ 99
const (
	RoleSuperAdmin = 1 // 超级管理员
	RoleAdmin      = 2 // 管理员
)

// 全局用户身份定义 100 ~ 199
const (
	RoleUser = 100 // 普通用户
)

// 其他业务身份定义 200+

// 检查角色是否为全局管理身份
func IsGlobalAdminRole(role int64) bool {
	return role >= 1 && role <= 99
}

// 检查角色是否为全局用户身份(包含管理员)
func IsGlobalUserRole(role int64) bool {
	return role >= 1 && role <= 199
}
