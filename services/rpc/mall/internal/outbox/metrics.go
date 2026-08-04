package outbox

import (
	"context"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"github.com/zeromicro/go-zero/core/service"

	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
)

var (
	outboxEventsGauge = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "budgetmatch",
		Subsystem: "mall_order_outbox",
		Name:      "events",
		Help:      "Current number of order outbox events by status and event type.",
		Labels:    []string{"status", "event_type"},
	})
	outboxOldestPendingGauge = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "budgetmatch",
		Subsystem: "mall_order_outbox",
		Name:      "oldest_pending_age_seconds",
		Help:      "Age in seconds of the oldest pending order outbox event.",
	})
	outboxPublishCounter = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "budgetmatch",
		Subsystem: "mall_order_outbox",
		Name:      "publish_total",
		Help:      "Order outbox publish attempts by event type and result.",
		Labels:    []string{"event_type", "result"},
	})
	outboxReplayCounter = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "budgetmatch",
		Subsystem: "mall_order_outbox",
		Name:      "replay_total",
		Help:      "Order outbox dead-letter replay attempts by result.",
		Labels:    []string{"result"},
	})
)

type MetricsStore interface {
	GetStats(ctx context.Context) ([]mall_order_outbox.StatusCount, time.Time, error)
}

type MetricsCollector struct {
	store    MetricsStore
	interval time.Duration
	quit     chan struct{}
	stopOnce sync.Once
}

func NewMetricsCollector(store MetricsStore, interval time.Duration) *MetricsCollector {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &MetricsCollector{store: store, interval: interval, quit: make(chan struct{})}
}

func RecordReplay(result string) {
	outboxReplayCounter.Inc(result)
}

func (c *MetricsCollector) Start() {
	if c.store == nil {
		return
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	c.collect()
	for {
		select {
		case <-ticker.C:
			c.collect()
		case <-c.quit:
			return
		}
	}
}

func (c *MetricsCollector) Stop() {
	c.stopOnce.Do(func() { close(c.quit) })
}

func (c *MetricsCollector) collect() {
	counts, oldestPending, err := c.store.GetStats(context.Background())
	if err != nil {
		logx.Errorf("collect outbox metrics failed: %v", err)
		return
	}
	// Reset all known combinations so a drained queue is reported as zero.
	for status := mall_order_outbox.StatusPending; status <= mall_order_outbox.StatusDead; status++ {
		for _, eventType := range []string{"created", "paid", "cancelled"} {
			outboxEventsGauge.Set(0, outboxStatusName(status), eventType)
		}
	}
	for _, count := range counts {
		outboxEventsGauge.Set(float64(count.Count), outboxStatusName(count.Status), count.EventType)
	}
	age := float64(0)
	if !oldestPending.IsZero() {
		age = time.Since(oldestPending).Seconds()
		if age < 0 {
			age = 0
		}
	}
	outboxOldestPendingGauge.Set(age)
}

func outboxStatusName(status int) string {
	switch status {
	case mall_order_outbox.StatusPending:
		return "pending"
	case mall_order_outbox.StatusProcessing:
		return "processing"
	case mall_order_outbox.StatusSent:
		return "sent"
	case mall_order_outbox.StatusDead:
		return "dead"
	default:
		return "unknown"
	}
}

var _ service.Service = (*MetricsCollector)(nil)
