package candidatecontract

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCandidateBounds(t *testing.T) {
	for _, ids := range [][]string{nil, {""}, {"a", "a"}, {" a"}, {"a "}, {"a\x00"}, {string([]byte{0xff})}, {strings.Repeat("x", 65)}} {
		require.False(t, ValidIDs(ids), "%q", ids)
	}
	ids := make([]string, MaxCandidates)
	for i := range ids {
		ids[i] = fmt.Sprintf("sku-%d", i)
	}
	require.True(t, ValidIDs(ids))
	require.False(t, ValidIDs(append(ids, "extra")))
	require.True(t, ValidIDs([]string{strings.Repeat("x", 64), "中文"}))
}
