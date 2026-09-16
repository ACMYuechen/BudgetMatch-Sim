package main

import "budgetmatch-sim/infra/interceptor"

// 用户网关使用的 GetActivity / ListActivities / ListSkusByActivity 仍需登录，
// 但不再要求管理员。所有配置写入及仅管理端使用的 GetSku 保持管理员权限。
func seckillAuthConfig(secret string) interceptor.AuthConfig {
	return interceptor.AuthConfig{
		Secret: secret,
		AdminMethods: map[string]struct{}{
			"/seckill.ActivityService/CreateActivity":  {},
			"/seckill.ActivityService/UpdateActivity":  {},
			"/seckill.ActivityService/DeleteActivity":  {},
			"/seckill.ActivityService/PreheatActivity": {},
			"/seckill.ActivityService/OnlineActivity":  {},
			"/seckill.ActivityService/OfflineActivity": {},
			"/seckill.SkuService/CreateSku":            {},
			"/seckill.SkuService/UpdateSku":            {},
			"/seckill.SkuService/GetSku":               {},
			"/seckill.SkuService/DeleteSku":            {},
		},
	}
}
