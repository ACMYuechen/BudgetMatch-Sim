package rag

import (
	"context"
	"sync"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/einolog"

	"github.com/cloudwego/eino/callbacks"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/threading"
)

// Syncer 是商品向量的后台同步器：启动即全量同步一次，之后按配置间隔周期同步。
// 首轮同步完成前向量表可能为空，检索为空时 provider 自动回退关键词链路，属预期降级。
type Syncer struct {
	pipeline *Pipeline
	interval time.Duration

	stop     chan struct{}
	stopOnce sync.Once
}

// NewSyncer 创建后台同步器。intervalSeconds 为负表示仅启动时同步一次。
func NewSyncer(pipeline *Pipeline, intervalSeconds int) *Syncer {
	var interval time.Duration
	if intervalSeconds > 0 {
		interval = time.Duration(intervalSeconds) * time.Second
	}
	return &Syncer{
		pipeline: pipeline,
		interval: interval,
		stop:     make(chan struct{}),
	}
}

// Start 启动后台同步 goroutine（panic 安全）。
func (s *Syncer) Start() {
	threading.GoSafe(func() {
		// 离线链路没有请求 ctx，主动注入日志 callbacks，Loader/Embedding/Indexer 同样可观测。
		ctx := callbacks.InitCallbacks(context.Background(),
			&callbacks.RunInfo{Name: "rag.sync"}, einolog.NewHandler())

		s.runOnce(ctx)
		if s.interval <= 0 {
			return
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.runOnce(ctx)
			case <-s.stop:
				return
			}
		}
	})
}

// Stop 停止周期同步，幂等。
func (s *Syncer) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// runOnce 执行一轮同步并记录统计；失败只告警，等待下一轮。
func (s *Syncer) runOnce(ctx context.Context) {
	start := time.Now()
	stats, err := s.pipeline.Sync(ctx)
	if err != nil {
		logx.Errorw("rag sync failed", logx.Field("error", err.Error()))
		return
	}
	logx.Infow("rag sync completed",
		logx.Field("loaded", stats.Loaded),
		logx.Field("indexed", stats.Indexed),
		logx.Field("refreshed", stats.Refreshed),
		logx.Field("pruned", stats.Pruned),
		logx.Field("duration_ms", time.Since(start).Milliseconds()),
	)
}
