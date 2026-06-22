package svc

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"budgetmatch-sim/services/rpc/seckill/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
)

const seckillConfigKey = "/config/seckill.rpc"

// DynamicConfig 相关错误。
var (
	ErrEmptyDynamicConfig       = errors.New("seckill dynamic config is empty")
	ErrInvalidDynamicConfigJSON = errors.New("seckill dynamic config is not valid JSON")
	ErrInvalidActivityRateLimit = errors.New("activity rate limit config is invalid")
	ErrInvalidUserRateLimit     = errors.New("user rate limit config is invalid")
	ErrInvalidLowStockThreshold = errors.New("low stock threshold must be greater than 0")
)

// dynamicState 封装动态配置的并发读写保护。
type dynamicState struct {
	mu  sync.RWMutex
	cfg config.DynamicConfig
}

func newDynamicState() *dynamicState {
	return &dynamicState{}
}

func (d *dynamicState) apply(cfg config.DynamicConfig) {
	d.mu.Lock()
	d.cfg = cfg
	d.mu.Unlock()
}

func (d *dynamicState) featureEnabled(name string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	switch name {
	case "enableNewOrderFlow":
		return d.cfg.Features.EnableNewOrderFlow
	default:
		return false
	}
}

func (d *dynamicState) lowStockThreshold() int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.LowStockThreshold
}

// loadDynamicConfig 把 etcd 中的 JSON 配置解析并应用到 ServiceContext。
// 空数据或解析失败时返回 error，不会回退到任何默认值。
func (s *ServiceContext) loadDynamicConfig(data []byte) error {
	if len(data) == 0 {
		return ErrEmptyDynamicConfig
	}

	var cfg config.DynamicConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		logx.Errorf("failed to unmarshal dynamic config: %v", err)
		return ErrInvalidDynamicConfigJSON
	}

	return s.applyDynamicConfig(cfg)
}

// applyDynamicConfig 校验并应用动态配置。任何字段不合法都返回 error。
func (s *ServiceContext) applyDynamicConfig(cfg config.DynamicConfig) error {
	if cfg.ActivityRateLimit.WindowSeconds <= 0 || cfg.ActivityRateLimit.Max <= 0 {
		return ErrInvalidActivityRateLimit
	}
	if cfg.UserRateLimit.Capacity <= 0 || cfg.UserRateLimit.Rate <= 0 || cfg.UserRateLimit.IntervalSeconds <= 0 {
		return ErrInvalidUserRateLimit
	}
	if cfg.LowStockThreshold <= 0 {
		return ErrInvalidLowStockThreshold
	}

	// 应用活动级滑动窗口限流参数
	s.ActivityRateLimiter.SetWindow(time.Duration(cfg.ActivityRateLimit.WindowSeconds) * time.Second)
	s.ActivityRateLimiter.SetMax(cfg.ActivityRateLimit.Max)

	// 应用用户级令牌桶限流参数
	s.UserRateLimiter.SetCapacity(cfg.UserRateLimit.Capacity)
	s.UserRateLimiter.SetRate(cfg.UserRateLimit.Rate)
	s.UserRateLimiter.SetInterval(time.Duration(cfg.UserRateLimit.IntervalSeconds) * time.Second)

	// 记录功能开关状态，业务逻辑中读取
	s.DynamicState.apply(cfg)

	logx.Infof("dynamic config applied: %+v", cfg)
	return nil
}

// IsFeatureEnabled 读取功能开关当前状态。
func (s *ServiceContext) IsFeatureEnabled(name string) bool {
	return s.DynamicState.featureEnabled(name)
}

// LowStockThreshold 返回触发 etcd 分布式锁兜底的低库存阈值。
func (s *ServiceContext) LowStockThreshold() int64 {
	return s.DynamicState.lowStockThreshold()
}
