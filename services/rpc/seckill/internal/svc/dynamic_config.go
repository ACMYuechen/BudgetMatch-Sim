package svc

import (
	"encoding/json"
	"sync"
	"time"

	"budgetmatch-sim/services/rpc/seckill/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
)

const seckillConfigKey = "/config/seckill.rpc"

// dynamicState 封装动态配置的并发读写保护。
type dynamicState struct {
	mu  sync.RWMutex
	cfg config.DynamicConfig
}

func newDynamicState() *dynamicState {
	return &dynamicState{cfg: config.DefaultDynamicConfig}
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
	if d.cfg.LowStockThreshold <= 0 {
		return 100
	}
	return d.cfg.LowStockThreshold
}

// loadDynamicConfig 把 etcd 中的 JSON 配置解析并应用到 ServiceContext。
func (s *ServiceContext) loadDynamicConfig(data []byte) {
	if len(data) == 0 {
		s.applyDynamicConfig(config.DefaultDynamicConfig)
		return
	}

	var cfg config.DynamicConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		logx.Errorf("failed to unmarshal dynamic config: %v, fallback to default", err)
		s.applyDynamicConfig(config.DefaultDynamicConfig)
		return
	}
	s.applyDynamicConfig(cfg)
}

func (s *ServiceContext) applyDynamicConfig(cfg config.DynamicConfig) {
	// 应用活动级滑动窗口限流参数
	if cfg.ActivityRateLimit.WindowSeconds > 0 && cfg.ActivityRateLimit.Max > 0 {
		s.ActivityRateLimiter.SetWindow(time.Duration(cfg.ActivityRateLimit.WindowSeconds) * time.Second)
		s.ActivityRateLimiter.SetMax(cfg.ActivityRateLimit.Max)
	}

	// 应用用户级令牌桶限流参数
	if cfg.UserRateLimit.Capacity > 0 && cfg.UserRateLimit.IntervalSeconds > 0 {
		s.UserRateLimiter.SetCapacity(cfg.UserRateLimit.Capacity)
		s.UserRateLimiter.SetRate(cfg.UserRateLimit.Rate)
		s.UserRateLimiter.SetInterval(time.Duration(cfg.UserRateLimit.IntervalSeconds) * time.Second)
	}

	// 记录功能开关状态，业务逻辑中读取
	s.DynamicState.apply(cfg)

	logx.Infof("dynamic config applied: %+v", cfg)
}

// IsFeatureEnabled 读取功能开关当前状态。
func (s *ServiceContext) IsFeatureEnabled(name string) bool {
	return s.DynamicState.featureEnabled(name)
}

// LowStockThreshold 返回触发 etcd 分布式锁兜底的低库存阈值。
func (s *ServiceContext) LowStockThreshold() int64 {
	return s.DynamicState.lowStockThreshold()
}
