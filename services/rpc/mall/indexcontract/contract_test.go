package indexcontract

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBudgetBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name        string
		initial     Budget
		items, size int
		allowed     bool
	}{
		{"page limit", Budget{}, 1, MaxPageBytes, true},
		{"page overflow", Budget{}, 1, MaxPageBytes + 1, false},
		{"item limit", Budget{Items: MaxItems - 1}, 1, 10, true},
		{"item overflow", Budget{Items: MaxItems}, 1, 10, false},
		{"byte limit", Budget{Bytes: MaxSnapshotBytes - 10}, 0, 10, true},
		{"terminal overflow", Budget{Bytes: MaxSnapshotBytes}, 0, 1, false},
		{"negative items", Budget{}, -1, 0, false},
		{"negative bytes", Budget{}, 0, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget := tc.initial
			require.Equal(t, tc.allowed, budget.Add(tc.items, tc.size))
			if tc.allowed {
				require.Equal(t, Budget{Items: tc.initial.Items + tc.items, Bytes: tc.initial.Bytes + tc.size}, budget)
			} else {
				require.Equal(t, tc.initial, budget)
			}
		})
	}
}
