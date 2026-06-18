package configcenter

import (
	"os"
	"strings"
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

func TestConfigCenter_WatchAndGet(t *testing.T) {
	hosts := etcdHosts()
	require.NotEmpty(t, hosts, "ETCD_HOSTS is required")

	cc, err := New(hosts)
	require.NoError(t, err)
	defer cc.Close()

	key := "/test/configcenter/watch"
	var called atomic.Int32
	loader := func(data []byte) error {
		called.Add(1)
		return nil
	}

	// clean up
	cc.cli.Delete(cc.cli.Ctx(), key)

	require.NoError(t, cc.Watch(key, loader))
	// wait for initial load callback
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(1), called.Load())

	// put a value
	_, err = cc.cli.Put(cc.cli.Ctx(), key, "hello")
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(2), called.Load())

	val, ok := cc.Get(key)
	assert.True(t, ok)
	assert.Equal(t, "hello", string(val))
}

func TestConfigCenter_NewWithEmptyEndpoints(t *testing.T) {
	cc, err := New(nil)
	assert.ErrorIs(t, err, ErrEmptyEndpoints)
	assert.Nil(t, cc)
}
