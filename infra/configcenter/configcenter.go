package configcenter

import (
	"context"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	clientv3 "go.etcd.io/etcd/client/v3"
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
// 如果 endpoints 为空，则返回 nil，调用方可以当作未启用动态配置处理。
func New(endpoints []string) (*ConfigCenter, error) {
	if len(endpoints) == 0 {
		return nil, nil
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
		return nil, nil
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
func (cc *ConfigCenter) Watch(key string, loader func([]byte)) {
	if cc == nil || cc.cli == nil {
		return
	}

	// 先加载一次当前值
	if value, err := cc.Load(key); err != nil {
		logx.Errorf("configcenter load key %s failed: %v", key, err)
	} else {
		loader(value)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cc.cancel = cancel

	go cc.watch(ctx, key, loader)
}

func (cc *ConfigCenter) watch(ctx context.Context, key string, loader func([]byte)) {
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
			loader(ev.Kv.Value)
		}
	}
}

// Close 关闭 etcd 客户端并停止监听。
func (cc *ConfigCenter) Close() error {
	if cc == nil || cc.cli == nil {
		return nil
	}
	if cc.cancel != nil {
		cc.cancel()
	}
	return cc.cli.Close()
}
