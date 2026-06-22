package config

// DynamicConfig 秒杀服务运行时动态可调的配置项（纯数据，不含锁）。
type DynamicConfig struct {
	ActivityRateLimit RateLimitConfig   `json:"activityRateLimit"`
	UserRateLimit     TokenBucketConfig `json:"userRateLimit"`
	Features          Features          `json:"features"`
	LowStockThreshold int64             `json:"lowStockThreshold"`
}

// RateLimitConfig 滑动窗口限流配置。
type RateLimitConfig struct {
	WindowSeconds int   `json:"windowSeconds"`
	Max           int64 `json:"max"`
}

// TokenBucketConfig 令牌桶限流配置。
type TokenBucketConfig struct {
	Capacity        int64 `json:"capacity"`
	Rate            int64 `json:"rate"`
	IntervalSeconds int64 `json:"intervalSeconds"`
}

// Features 功能开关。
type Features struct {
	EnableNewOrderFlow bool `json:"enableNewOrderFlow"`
}
