package dlock

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func etcdHosts() []string {
	env := os.Getenv("ETCD_HOSTS")
	if env == "" {
		return nil
	}
	return strings.Split(env, ",")
}

func TestEtcdLock_Concurrent(t *testing.T) {
	hosts := etcdHosts()
	require.NotEmpty(t, hosts, "ETCD_HOSTS is required")

	mgr, err := NewManager(hosts)
	require.NoError(t, err)
	defer mgr.Close()

	var counter atomic.Int32
	var maxConcurrent atomic.Int32
	var current atomic.Int32
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for range 5 {
		wg.Go(func() {
			lock, err := mgr.NewLock("/test/dlock/concurrent", 10)
			require.NoError(t, err)
			defer lock.Close()

			err = lock.Lock(ctx)
			require.NoError(t, err)

			c := current.Add(1)
			for {
				m := maxConcurrent.Load()
				if c > m {
					if maxConcurrent.CompareAndSwap(m, c) {
						continue
					}
				}
				break
			}

			counter.Add(1)
			time.Sleep(50 * time.Millisecond)
			current.Add(-1)

			err = lock.Unlock(ctx)
			require.NoError(t, err)
		})
	}

	wg.Wait()
	assert.Equal(t, int32(5), counter.Load())
	assert.Equal(t, int32(1), maxConcurrent.Load())
}

func TestEtcdLock_TryLockTimeout(t *testing.T) {
	hosts := etcdHosts()
	require.NotEmpty(t, hosts, "ETCD_HOSTS is required")

	mgr, err := NewManager(hosts)
	require.NoError(t, err)
	defer mgr.Close()

	lock1, err := mgr.NewLock("/test/dlock/timeout", 10)
	require.NoError(t, err)
	defer lock1.Close()

	require.NoError(t, lock1.Lock(context.Background()))

	lock2, err := mgr.NewLock("/test/dlock/timeout", 10)
	require.NoError(t, err)
	defer lock2.Close()

	err = lock2.TryLock(200 * time.Millisecond)
	assert.Error(t, err)

	require.NoError(t, lock1.Unlock(context.Background()))
	require.NoError(t, lock2.TryLock(200*time.Millisecond))
}

func TestNewManager_WithEmptyEndpoints(t *testing.T) {
	mgr, err := NewManager(nil)
	assert.ErrorIs(t, err, ErrEmptyEndpoints)
	assert.Nil(t, mgr)
}

func TestWithLock_NilLock(t *testing.T) {
	err := WithLock(context.Background(), nil, func() error {
		return nil
	})
	assert.ErrorIs(t, err, ErrNilManager)
}
