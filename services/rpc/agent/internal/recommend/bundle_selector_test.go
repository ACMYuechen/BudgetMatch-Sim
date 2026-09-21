package recommend

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

func FuzzBundleSelectorHardConstraints(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 255, 255, 255, 255, 255, 255, 255, 255})
	f.Add(make([]byte, 32))
	f.Add([]byte{1, 1, 0, 0, 0, 0, 0, 0, 2, 1, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 {
			return
		}
		budget := int64(binary.LittleEndian.Uint64(data[:8])%uint64(agent.MaxBudgetCents)) + 1
		count := int32(data[0]%10) + 1
		var candidates []agent.ProductCandidate
		for offset := 0; offset+8 <= len(data) && len(candidates) < 100; offset += 8 {
			value := int64(binary.LittleEndian.Uint64(data[offset : offset+8]))
			candidates = append(candidates, agent.ProductCandidate{
				Id: fmt.Sprintf("sku-%d", data[offset]%8), PriceCents: value, Stock: int64(int8(data[offset+1])),
			})
		}
		items, total := NewBundleSelector().Select(candidates, agent.Intent{BudgetCents: budget, MaxItems: count})
		limits := agent.Constraints{BudgetCents: budget, MaxItems: count}
		if err := limits.ValidateResult(&agent.Result{Items: items, TotalPriceCents: total, Candidates: candidates}); err != nil {
			t.Fatalf("selector violated hard constraints: %v", err)
		}
	})
}

func TestGuardrailsSelectorRejectsInvalidAndDuplicateCandidates(t *testing.T) {
	input := []agent.ProductCandidate{
		{Id: "a", PriceCents: 100, Stock: 1},
		{Id: "a", PriceCents: 100, Stock: 1},
		{Id: "", PriceCents: 1, Stock: 1},
		{Id: "negative", PriceCents: -10, Stock: 1},
		{Id: "free", PriceCents: 0, Stock: 1},
		{Id: "overflow", PriceCents: math.MaxInt64, Stock: 1},
		{Id: "sold-out", PriceCents: 10, Stock: 0},
	}
	items, total := NewBundleSelector().Select(input, agent.Intent{BudgetCents: 500, MaxItems: 10})
	if len(items) != 1 || items[0].Id != "a" || total != 100 {
		t.Fatalf("unsafe selection: %+v, total=%d", items, total)
	}
	if input[0].Id != "a" || input[2].Id != "" {
		t.Fatal("selector modified caller's candidate slice")
	}
}

func TestGuardrailsLatestUnavailableCandidateInvalidatesOldSnapshot(t *testing.T) {
	items, total := NewBundleSelector().Select([]agent.ProductCandidate{
		{Id: "a", PriceCents: 100, Stock: 1},
		{Id: "a", PriceCents: 100, Stock: 0},
	}, agent.Intent{BudgetCents: 500, MaxItems: 3})
	if len(items) != 0 || total != 0 {
		t.Fatalf("selected stale available snapshot: %+v, %d", items, total)
	}
}
