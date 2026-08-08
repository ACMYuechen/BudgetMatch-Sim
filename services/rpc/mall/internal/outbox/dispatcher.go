package outbox

import (
	"context"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"

	"budgetmatch-sim/infra/rocketmq"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
)

type Config struct {
	PollInterval time.Duration
	BatchSize    int
	LockDuration time.Duration
	SendTimeout  time.Duration
	BaseBackoff  time.Duration
	MaxBackoff   time.Duration
}

type Store interface {
	ClaimBatch(ctx context.Context, limit int, now, lockedUntil time.Time) ([]mall_order_outbox.MallOrderOutbox, int64, error)
	MarkSent(ctx context.Context, id string, attempt int, publishedAt time.Time) (bool, error)
	MarkRetry(ctx context.Context, req *mall_order_outbox.MarkRetryReq) (bool, error)
	MarkDead(ctx context.Context, req *mall_order_outbox.MarkDeadReq) (bool, error)
}

type Dispatcher struct {
	store    Store
	producer rocketmq.Producer
	factory  func() (rocketmq.Producer, error)
	owned    bool
	config   Config
	quit     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
}

func DefaultConfig() Config {
	return Config{
		PollInterval: time.Second,
		BatchSize:    20,
		LockDuration: 2 * time.Minute,
		SendTimeout:  5 * time.Second,
		BaseBackoff:  time.Second,
		MaxBackoff:   5 * time.Minute,
	}
}

func NewDispatcher(store Store, producer rocketmq.Producer, config Config) *Dispatcher {
	return &Dispatcher{store: store, producer: producer, config: config, quit: make(chan struct{})}
}

// NewResilientDispatcher 创建一个具有自动重连能力的消息调度器。
func NewResilientDispatcher(store Store, mqConfig rocketmq.Config, config Config) *Dispatcher {
	return &Dispatcher{
		store:   store,
		factory: func() (rocketmq.Producer, error) { return rocketmq.NewProducer(mqConfig) },
		owned:   true,
		config:  config,
		quit:    make(chan struct{}),
	}
}

func (d *Dispatcher) Start() {
	if d.store == nil || (d.producer == nil && d.factory == nil) {
		logx.Error("outbox dispatcher not started: store and producer are unavailable")
		return
	}

	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()
	logx.Info("outbox dispatcher started")

	d.dispatchOnce()
	for {
		select {
		case <-ticker.C:
			d.dispatchOnce()
		case <-d.quit:
			logx.Info("outbox dispatcher stopped")
			return
		}
	}
}

func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() {
		close(d.quit)
		d.resetProducer()
	})
}

func (d *Dispatcher) dispatchOnce() {
	if d.currentProducer() == nil {
		if err := d.connectProducer(); err != nil {
			logx.Errorf("connect outbox rocketmq producer failed: error=%v", err)
			return
		}
	}
	now := time.Now()
	list, expiredDeadCount, err := d.store.ClaimBatch(context.Background(), d.config.BatchSize, now, now.Add(d.config.LockDuration))
	if err != nil {
		logx.Errorf("claim outbox events failed: error=%v", err)
		return
	}
	if expiredDeadCount > 0 {
		logx.Errorf("expired outbox events moved to dead state: count=%d", expiredDeadCount)
	}
	for i := range list {
		d.publish(&list[i])
	}
}

func (d *Dispatcher) publish(event *mall_order_outbox.MallOrderOutbox) {
	producer := d.currentProducer()
	if producer == nil {
		outboxPublishCounter.Inc(event.EventType, "failed")
		d.handleFailure(event, context.Canceled)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), d.config.SendTimeout)
	_, err := producer.SendSync(ctx, &rocketmq.Message{Topic: event.Topic, Tag: event.Tag, Keys: []string{event.MessageKey}, Body: []byte(event.Payload)})
	cancel()
	if err != nil {
		outboxPublishCounter.Inc(event.EventType, "failed")
		if d.owned {
			d.resetProducer()
		}
		d.handleFailure(event, err)
		return
	}
	outboxPublishCounter.Inc(event.EventType, "sent")

	now := time.Now()
	ok, err := d.store.MarkSent(context.Background(), event.Id, event.Attempts, now)
	if err != nil {
		logx.Errorf("mark outbox event sent failed: event_id=%s attempt=%d error=%v", event.Id, event.Attempts, err)
		return
	}
	if !ok {
		logx.Errorf("mark outbox event sent affected no rows: event_id=%s attempt=%d", event.Id, event.Attempts)
		return
	}
	logx.Infof("outbox event sent: event_id=%s topic=%s aggregate_id=%s attempt=%d", event.Id, event.Topic, event.AggregateId, event.Attempts)
}

func (d *Dispatcher) currentProducer() rocketmq.Producer {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.producer
}

func (d *Dispatcher) connectProducer() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.producer != nil || d.factory == nil {
		return nil
	}
	producer, err := d.factory()
	if err != nil {
		return err
	}
	d.producer = producer
	return nil
}

func (d *Dispatcher) resetProducer() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.producer != nil && d.owned {
		if err := d.producer.Shutdown(); err != nil {
			logx.Errorf("shutdown outbox rocketmq producer failed: %v", err)
		}
		d.producer = nil
	}
}

func (d *Dispatcher) handleFailure(event *mall_order_outbox.MallOrderOutbox, sendErr error) {
	now := time.Now()
	lastError := truncateError(sendErr.Error())
	if event.Attempts >= event.MaxAttempts {
		ok, err := d.store.MarkDead(context.Background(), &mall_order_outbox.MarkDeadReq{Id: event.Id, Attempt: event.Attempts, LastError: lastError, Now: now})
		if err != nil {
			logx.Errorf("mark outbox event dead failed: event_id=%s attempt=%d error=%v", event.Id, event.Attempts, err)
			return
		}
		if !ok {
			logx.Errorf("mark outbox event dead affected no rows: event_id=%s attempt=%d", event.Id, event.Attempts)
			return
		}
		logx.Errorf("outbox event moved to dead state: event_id=%s topic=%s aggregate_id=%s attempts=%d error=%v", event.Id, event.Topic, event.AggregateId, event.Attempts, sendErr)
		return
	}

	nextRetryAt := now.Add(retryBackoff(event.Attempts, d.config.BaseBackoff, d.config.MaxBackoff))
	ok, err := d.store.MarkRetry(context.Background(), &mall_order_outbox.MarkRetryReq{Id: event.Id, Attempt: event.Attempts, NextRetryAt: nextRetryAt, LastError: lastError, Now: now})
	if err != nil {
		logx.Errorf("mark outbox event retry failed: event_id=%s attempt=%d error=%v", event.Id, event.Attempts, err)
		return
	}
	if !ok {
		logx.Errorf("mark outbox event retry affected no rows: event_id=%s attempt=%d", event.Id, event.Attempts)
		return
	}
	logx.Errorf("outbox event send failed and scheduled retry: event_id=%s topic=%s aggregate_id=%s attempt=%d next_retry_at=%s error=%v", event.Id, event.Topic, event.AggregateId, event.Attempts, nextRetryAt.Format(time.RFC3339), sendErr)
}

func retryBackoff(attempt int, base, maximum time.Duration) time.Duration {
	if attempt <= 1 {
		return base
	}
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func truncateError(message string) string {
	const maxLength = 2048
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength]
}

var _ service.Service = (*Dispatcher)(nil)
