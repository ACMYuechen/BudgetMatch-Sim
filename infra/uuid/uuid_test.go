package uuid

import (
	"encoding/base64"
	"strings"
	"testing"

	googleuuid "github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUUID(t *testing.T) {
	id := NewUUID()

	parsed, err := googleuuid.Parse(id)
	require.NoError(t, err)
	assert.Equal(t, googleuuid.Version(4), parsed.Version())
	assert.Equal(t, googleuuid.RFC4122, parsed.Variant())
}

func TestNewShortUUID(t *testing.T) {
	id := NewShortUUID()

	require.Len(t, id, 22)
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	require.NoError(t, err)
	require.Len(t, decoded, 16)

	parsed, err := googleuuid.FromBytes(decoded)
	require.NoError(t, err)
	assert.Equal(t, googleuuid.Version(4), parsed.Version())
	assert.Equal(t, googleuuid.RFC4122, parsed.Variant())
}

func TestNewPrefixedShortUUID(t *testing.T) {
	const prefix = "usr_"

	id := NewPrefixedShortUUID(prefix)

	require.True(t, strings.HasPrefix(id, prefix))
	require.Len(t, strings.TrimPrefix(id, prefix), 22)
}

func TestNewShortUUID_Unique(t *testing.T) {
	const count = 10_000
	seen := make(map[string]struct{}, count)
	for range count {
		id := NewShortUUID()
		if _, exists := seen[id]; exists {
			t.Fatalf("generated duplicate short UUID: %s", id)
		}
		seen[id] = struct{}{}
	}
}
