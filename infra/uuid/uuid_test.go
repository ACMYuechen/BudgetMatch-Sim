package uuid

import (
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"

	googleuuid "github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	id := New()

	parsed, err := googleuuid.Parse(id)
	require.NoError(t, err)
	assert.Equal(t, googleuuid.Version(4), parsed.Version())
	assert.Equal(t, googleuuid.RFC4122, parsed.Variant())
}

func TestNewShort(t *testing.T) {
	id := NewShort()

	require.Len(t, id, ShortLength)
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	require.NoError(t, err)
	require.Len(t, decoded, 16)

	parsed, err := googleuuid.FromBytes(decoded)
	require.NoError(t, err)
	assert.Equal(t, googleuuid.Version(4), parsed.Version())
	assert.Equal(t, googleuuid.RFC4122, parsed.Variant())
}

func TestNewPrefixedShort(t *testing.T) {
	id := NewPrefixedShort("prod")

	prefix := "prod" + PrefixSeparator
	require.True(t, strings.HasPrefix(id, prefix))
	require.Len(t, strings.TrimPrefix(id, prefix), ShortLength)
	require.Len(t, id, len(prefix)+ShortLength)
}

func TestNewPrefixedShortGenerator(t *testing.T) {
	generator, err := NewPrefixedShortGenerator("sord")
	require.NoError(t, err)

	first := generator()
	second := generator()
	assert.True(t, strings.HasPrefix(first, "sord_"))
	assert.True(t, strings.HasPrefix(second, "sord_"))
	assert.NotEqual(t, first, second)
}

func TestValidatePrefix(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		wantErr bool
	}{
		{name: "letters", prefix: "usr"},
		{name: "letters and digits", prefix: "sku2"},
		{name: "minimum length", prefix: "ab"},
		{name: "maximum length", prefix: "abcdefghijkl"},
		{name: "empty", prefix: "", wantErr: true},
		{name: "too short", prefix: "a", wantErr: true},
		{name: "too long", prefix: "abcdefghijklm", wantErr: true},
		{name: "starts with digit", prefix: "1sku", wantErr: true},
		{name: "uppercase", prefix: "SKU", wantErr: true},
		{name: "underscore", prefix: "mall_order", wantErr: true},
		{name: "hyphen", prefix: "mall-order", wantErr: true},
		{name: "non ASCII", prefix: "用户", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePrefix(tt.prefix)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidPrefix)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestNewPrefixedShortGenerator_InvalidPrefix(t *testing.T) {
	generator, err := NewPrefixedShortGenerator("INVALID")

	assert.Nil(t, generator)
	assert.True(t, errors.Is(err, ErrInvalidPrefix))
}

func TestMustNewPrefixedShortGenerator_InvalidPrefix(t *testing.T) {
	assert.Panics(t, func() {
		MustNewPrefixedShortGenerator("INVALID")
	})
}

func TestNewShort_Unique(t *testing.T) {
	const count = 10_000
	seen := make(map[string]struct{}, count)
	for range count {
		id := NewShort()
		if _, exists := seen[id]; exists {
			t.Fatalf("generated duplicate short UUID: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerator_Concurrent(t *testing.T) {
	const (
		workers   = 16
		perWorker = 500
	)

	generator := MustNewPrefixedShortGenerator("pay")
	ids := make(chan string, workers*perWorker)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWorker {
				ids <- generator()
			}
		}()
	}

	wg.Wait()
	close(ids)

	seen := make(map[string]struct{}, workers*perWorker)
	for id := range ids {
		assert.True(t, strings.HasPrefix(id, "pay_"))
		if _, exists := seen[id]; exists {
			t.Fatalf("generated duplicate prefixed short UUID: %s", id)
		}
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, workers*perWorker)
}
