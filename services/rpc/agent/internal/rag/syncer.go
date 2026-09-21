package rag

import (
	"context"
	"errors"
	"sync"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/einolog"
	"budgetmatch-sim/services/rpc/agent/internal/safety"

	"github.com/cloudwego/eino/callbacks"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/threading"
)

// Syncer 是商品向量的后台同步器：启动即全量同步一次，之后按配置间隔周期同步。
// 首轮同步完成前向量表可能为空，检索为空时 provider 自动回退关键词链路，属预期降级。
type Syncer struct {
	pipeline    synchronizer
	interval    time.Duration
	runTimeout  time.Duration
	stopTimeout time.Duration

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

type synchronizer interface {
	Sync(context.Context) (SyncStats, error)
}

// NewSyncer 创建后台同步器，单轮和关闭均有界。负间隔表示仅启动同步一次。
func NewSyncer(pipeline synchronizer, cfg Config) *Syncer {
	cfg = cfg.Normalize()
	var interval time.Duration
	if cfg.SyncIntervalSeconds > 0 {
		seconds := min(int64(cfg.SyncIntervalSeconds), int64((1<<63-1)/time.Second))
		interval = time.Duration(seconds) * time.Second
	}
	return &Syncer{
		pipeline:    pipeline,
		interval:    interval,
		runTimeout:  time.Duration(cfg.SyncTimeoutSeconds) * time.Second,
		stopTimeout: time.Duration(cfg.SyncStopTimeoutSeconds) * time.Second,
		done:        make(chan struct{}),
	}
}

// Start 幂等；Stop/Shutdown 之后不能重新启动。
func (s *Syncer) Start() {
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.started = true
	s.mu.Unlock()
	threading.GoSafe(func() {
		defer close(s.done)
		defer cancel()
		// Do not let a dependency panic dump raw errors, documents or credentials.
		defer func() {
			if recover() != nil {
				logx.Errorw("rag sync worker stopped after panic", logx.Field("error_code", "internal"))
			}
		}()
		// 离线链路没有请求 ctx，主动注入日志 callbacks，Loader/Embedding/Indexer 同样可观测。
		ctx = callbacks.InitCallbacks(ctx,
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
			case <-ctx.Done():
				return
			}
		}
	})
}

// Stop 适配进程关闭钩子：取消当前轮，最多等待配置的关闭时间。
func (s *Syncer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), s.stopTimeout)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		logx.Errorw("rag sync shutdown timed out", logx.Field("error_code", safety.ErrorCode(err)))
	}
}

// Shutdown cancels immediately and waits for worker exit within the caller's
// deadline. An uncooperative dependency cannot be forcibly killed; it remains
// canceled and cannot schedule another round after it eventually returns.
func (s *Syncer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		if !s.started {
			close(s.done)
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return nil
	default:
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runOnce 执行一轮同步并记录统计；失败只告警，等待下一轮。
func (s *Syncer) runOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, s.runTimeout)
	defer cancel()
	start := time.Now()
	batchID := uuid.NewString()
	logx.Infow("rag sync started", logx.Field("batch_id", batchID), logx.Field("started_at", start.UTC().Format(time.RFC3339Nano)))
	stats, err := s.pipeline.Sync(ctx)
	fields := []logx.LogField{
		logx.Field("batch_id", batchID),
		logx.Field("finished_at", time.Now().UTC().Format(time.RFC3339Nano)),
		logx.Field("loaded", stats.Loaded),
		logx.Field("indexed", stats.Indexed),
		logx.Field("refreshed", stats.Refreshed),
		logx.Field("pruned", stats.Pruned),
		logx.Field("duration_ms", time.Since(start).Milliseconds()),
	}
	if errors.Is(err, ErrSyncBusy) {
		logx.Infow("rag sync skipped: index synchronization busy", fields...)
	} else if err != nil {
		logx.Errorw("rag sync failed", append(fields, logx.Field("error_code", safety.ErrorCode(err)))...)
	} else {
		logx.Infow("rag sync completed", fields...)
	}
}
