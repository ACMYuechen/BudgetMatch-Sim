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

func etcdEndpoints() []string {
	env := os.Getenv("ETCD_ENDPOINTS")
	if env == "" {
		return nil
	}
	return strings.Split(env, ",")
}

func TestConfigCenter_WatchAndGet(t *testing.T) {
	endpoints := etcdEndpoints()
	if len(endpoints) == 0 {
		t.Skip("ETCD_ENDPOINTS not set, skip integration test")
	}

	cc, err := New(endpoints)
	require.NoError(t, err)
	defer cc.Close()

	key := "/test/configcenter/watch"
	var called atomic.Int32
	loader := func(data []byte) {
		called.Add(1)
	}

	// clean up
	cc.cli.Delete(cc.cli.Ctx(), key)

	cc.Watch(key, loader)
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
	assert.NoError(t, err)
	assert.Nil(t, cc)
}
