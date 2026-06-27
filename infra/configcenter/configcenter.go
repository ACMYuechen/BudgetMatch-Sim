package configcenter

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	ErrNilConfigCenter = errors.New("configcenter is nil")
	ErrEmptyEndpoints  = errors.New("etcd endpoints are required")
)

// ConfigCenter 基于 etcd 的动态配置中心封装。
// 每个服务可以持有一个实例，监听属于自己的配置 key（如 /config/seckill.rpc）。
type ConfigCenter struct {
	cli    *clientv3.Client
	mu     sync.RWMutex
	values map[string][]byte
	cancel context.CancelFunc
}

// New 创建一个配置中心实例。
// endpoints 为空时返回错误，调用方必须保证 etcd 已配置。
func New(endpoints []string) (*ConfigCenter, error) {
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

	return &ConfigCenter{
		cli:    cli,
		values: make(map[string][]byte),
	}, nil
}

// Load 从 etcd 读取指定 key 的最新值并缓存。
func (cc *ConfigCenter) Load(key string) ([]byte, error) {
	if cc == nil || cc.cli == nil {
		return nil, ErrNilConfigCenter
	}

	resp, err := cc.cli.Get(context.Background(), key)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, nil
	}

	value := resp.Kvs[0].Value
	cc.mu.Lock()
	cc.values[key] = value
	cc.mu.Unlock()
	return value, nil
}

// Get 获取本地缓存的 key 值。
func (cc *ConfigCenter) Get(key string) ([]byte, bool) {
	if cc == nil {
		return nil, false
	}
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	v, ok := cc.values[key]
	return v, ok
}

// Watch 监听指定 key 的变化，首次会立即加载一次当前值，后续变更通过 loader 回调通知。
// loader 返回 error 时，Watch 会在日志中记录但不会终止监听；首次加载失败会直接返回 error。
func (cc *ConfigCenter) Watch(key string, loader func([]byte) error) error {
	if cc == nil || cc.cli == nil {
		return ErrNilConfigCenter
	}

	// 先加载一次当前值
	value, err := cc.Load(key)
	if err != nil {
		return err
	}
	if err := loader(value); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	cc.cancel = cancel

	go cc.watch(ctx, key, loader)
	return nil
}

func (cc *ConfigCenter) watch(ctx context.Context, key string, loader func([]byte) error) {
	watchChan := cc.cli.Watch(ctx, key)
	for wresp := range watchChan {
		if wresp.Err() != nil {
			logx.Errorf("configcenter watch key %s error: %v", key, wresp.Err())
			continue
		}
		for _, ev := range wresp.Events {
			cc.mu.Lock()
			cc.values[key] = ev.Kv.Value
			cc.mu.Unlock()
			if err := loader(ev.Kv.Value); err != nil {
				logx.Errorf("configcenter loader failed for key %s: %v", key, err)
			}
		}
	}
}

// Close 关闭 etcd 客户端并停止监听。
func (cc *ConfigCenter) Close() error {
	if cc == nil || cc.cli == nil {
		return ErrNilConfigCenter
	}
	if cc.cancel != nil {
		cc.cancel()
	}
	return cc.cli.Close()
}
