package dlock

import (
	"context"
	"errors"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

var (
	ErrLockNotHeld     = errors.New("lock not held")
	ErrNilManager      = errors.New("etcd lock manager is nil")
	ErrEmptyEndpoints  = errors.New("etcd endpoints are required for distributed lock")
)

// Manager 管理一组共享同一个 etcd 连接的分布式锁。
type Manager struct {
	cli *clientv3.Client
}

// NewManager 创建一个基于 etcd 的分布式锁管理器。
// endpoints 为空时返回错误，调用方必须保证 etcd 已配置。
func NewManager(endpoints []string) (*Manager, error) {
	if len(endpoints) == 0 {
		return nil, ErrEmptyEndpoints
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return &Manager{cli: cli}, nil
}

// NewLock 创建一个指定 key 的分布式锁。
// ttl 为 session 存活秒数，默认 60。
func (m *Manager) NewLock(key string, ttl int) (*EtcdLock, error) {
	if m == nil || m.cli == nil {
		return nil, ErrNilManager
	}
	if ttl <= 0 {
		ttl = 60
	}

	s, err := concurrency.NewSession(m.cli, concurrency.WithTTL(ttl))
	if err != nil {
		return nil, err
	}

	mutex := concurrency.NewMutex(s, key)
	return &EtcdLock{
		cli:   m.cli,
		s:     s,
		mutex: mutex,
	}, nil
}

// Close 关闭 etcd 客户端。
func (m *Manager) Close() error {
	if m == nil || m.cli == nil {
		return ErrNilManager
	}
	return m.cli.Close()
}

// EtcdLock 基于 etcd concurrency.Mutex 的分布式锁封装。
type EtcdLock struct {
	cli   *clientv3.Client
	s     *concurrency.Session
	mutex *concurrency.Mutex
}

// Lock 阻塞获取锁，直到成功或上下文取消。
func (l *EtcdLock) Lock(ctx context.Context) error {
	if l == nil || l.mutex == nil {
		return ErrNilManager
	}
	return l.mutex.Lock(ctx)
}

// TryLock 尝试在指定超时内获取锁。
func (l *EtcdLock) TryLock(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return l.Lock(ctx)
}

// Unlock 释放锁。
func (l *EtcdLock) Unlock(ctx context.Context) error {
	if l == nil || l.mutex == nil {
		return ErrLockNotHeld
	}
	return l.mutex.Unlock(ctx)
}

// Close 关闭锁会话。
func (l *EtcdLock) Close() error {
	if l == nil || l.s == nil {
		return ErrNilManager
	}
	return l.s.Close()
}

// WithLock 在持有锁期间执行 fn，自动加锁/解锁并处理错误。
// lock 为 nil 时返回 ErrNilManager，不会无保护执行 fn。
func WithLock(ctx context.Context, lock *EtcdLock, fn func() error) error {
	if lock == nil {
		return ErrNilManager
	}
	if err := lock.Lock(ctx); err != nil {
		logx.Errorf("etcd lock acquire failed: %v", err)
		return err
	}
	defer func() {
		if err := lock.Unlock(ctx); err != nil {
			logx.Errorf("etcd lock release failed: %v", err)
		}
	}()
	return fn()
}
