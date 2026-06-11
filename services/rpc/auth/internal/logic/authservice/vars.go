package authservicelogic

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
