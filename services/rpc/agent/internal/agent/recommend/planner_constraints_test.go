package recommend

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

func TestPlannerRejectsInvalidTextConstraints(t *testing.T) {
	for _, query := range []string{
		"预算 $500 买手机", "budget 500 USD", "预算 500 美元", "预算 EUR 500",
		"预算500日元", "预算500港元", "预算€500", "预算500英镑",
		"预算 -500 元", "预算负500元", "预算￥-500", "预算-￥500", "预算 0 元",
		"预算 0.001 元", "预算19.999元", "预算 0.000001 千元",
		"预算3000元，预算5000元", "预算3000元，不超过2000元", "预算5000-3000元",
		"预算 3k-5k USD", "预算 3000 元到 5000 美元", "预算 1,00 元", "预算 1.2.3 元",
		"预算 1e3 元", "预算三千元", "预算 0-5000 元",
		"最多0件", "最多-2件", "最多负两件", "最多1.5件", "最多十一件", "不超过15件商品",
		"最多2件，最多3件", "最多2-3件", "至少3件", "预算至少3000元",
		"预算USD500", "预算JPY500", "预算5百元", "预算500xyz", "预算2keyboard",
		"预算3000或5000", "预算3000元或者5000元", "budget 3000 or 5000",
		"最多负 两件", "最多- two items", "买一台电脑和一部手机",
		"预算NaN元", "预算Inf元", "预算XYZ500", "取消预算", "预算不限", "预算不是5000元",
		"budget at   least 3000", "at   least 3 items",
		"预算3000元，改为5000元", "预算5000元起", "不低于5000元", "最低3件", "3件起",
	} {
		t.Run(query, func(t *testing.T) {
			_, err := NewPlanner().Resolve(agent.Input{Query: query}, nil)
			if !errors.Is(err, agent.ErrInvalidInput) {
				t.Fatalf("invalid text accepted: %q, err=%v", query, err)
			}
		})
	}
}

func TestPlannerResolvesTextConstraints(t *testing.T) {
	for _, tc := range []struct {
		query  string
		budget int64
		items  int32
	}{
		{"预算5元，最多两件", 500, 2},
		{"预算0.01元，只买一件", 1, 1},
		{"预算1,000.50元，最多十件", 100050, 10},
		{"预算人民币500元，不超过三件", 50000, 3},
		{"budget CNY 500, at most two items", 50000, 2},
		{"budget 500 RMB, up to 4 pieces", 50000, 4},
		{"预算１２００元，最多２件", 120000, 2},
		{"预算3-5k，最多2件", 500000, 2},
		{"预算3000-5000元，最多两件", 500000, 2},
		{"预算1.001千元，最多一件", 100100, 1},
		{"预算3000元，预算3000元，最多2件，最多2件", 300000, 2},
		{"预算1000元，耳机价格300-500元，最多2件", 100000, 2},
		{"买2个iPhone 15，配4K显示器和65W充电器", 300000, 2},
		{"买 iPhone 15 配件", 300000, 3},
		{"买一套学习桌面", 300000, 3},
		{"推荐三件套床品", 300000, 3},
		{"预算CNY500，最多两件", 50000, 2},
		{"预算0.000001万元，最多两件", 1, 2},
		{"预算3k-5000元，最多三件，买2个耳机", 500000, 3},
		{"budget 3000 to 5000, at most two items", 500000, 2},
		{"预算改成50元", 5000, 3},
		{"预算降到30元", 3000, 3},
		{"预算降低至19.99元", 1999, 3},
		{"预算减少到1,000.50元", 100050, 3},
		{"预算下调为0.000001万元", 1, 3},
		{"预算改成30-50元，只买一件鼠标", 5000, 1},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, err := NewPlanner().Resolve(agent.Input{Query: tc.query}, nil)
			if err != nil || got.BudgetCents != tc.budget || got.MaxItems != tc.items {
				t.Fatalf("Resolve = %+v, %v; want budget=%d items=%d", got, err, tc.budget, tc.items)
			}
		})
	}
}

func TestPlannerBudgetChangeKeepsValidationAndPrecedence(t *testing.T) {
	prior := &agent.Intent{BudgetCents: 20000, MaxItems: 2}
	for _, query := range []string{"预算改成50元", "预算降到50元", "预算降低至50元", "预算减少到50元", "预算下调为50元"} {
		got, err := NewPlanner().Resolve(agent.Input{Query: query, PriorIntent: prior}, []string{"预算300元"})
		if err != nil || got.BudgetCents != 5000 || got.MaxItems != 2 || prior.BudgetCents != 20000 {
			t.Fatalf("change did not override only budget: %+v %v", got, err)
		}
		got, err = NewPlanner().Resolve(agent.Input{Query: query, BudgetCents: 8000, PriorIntent: prior}, nil)
		if err != nil || got.BudgetCents != 8000 {
			t.Fatalf("text replaced structured budget: %+v %v", got, err)
		}
	}
	for _, tc := range []struct {
		query string
		want  error
	}{
		{"预算改成0元", agent.ErrBudgetText}, {"预算降到-30元", agent.ErrBudgetText},
		{"预算降到19.999元", agent.ErrBudgetText}, {"预算降低至1000000001元", agent.ErrBudgetText},
		{"预算改成30或50元", agent.ErrBudgetText}, {"预算改成50元，预算降到30元", agent.ErrBudgetText},
		{"预算改成50美元", agent.ErrBudgetCurrency}, {"预算降到JPY30", agent.ErrBudgetCurrency},
		// 相对变化不是绝对上限，不能把“减少了 50”误当作新预算 50。
		{"预算减少50元", agent.ErrBudgetText}, {"预算降低了50元", agent.ErrBudgetText},
		{"预算下调50%", agent.ErrBudgetText}, {"预算改成不限", agent.ErrBudgetText},
	} {
		if _, err := NewPlanner().Resolve(agent.Input{Query: tc.query, PriorIntent: prior}, nil); !errors.Is(err, tc.want) {
			t.Fatalf("%q: got %v, want %v", tc.query, err, tc.want)
		}
	}
}

func TestPlannerTextErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  error
	}{
		{"预算$500", agent.ErrBudgetCurrency},
		{"预算JPY500", agent.ErrBudgetCurrency},
		{"预算500元到600美元", agent.ErrBudgetCurrency},
		{"预算-500元", agent.ErrBudgetText},
		{"预算19.999元", agent.ErrBudgetText},
		{"预算3000或5000", agent.ErrBudgetText},
		{"最多0件", agent.ErrItemLimitText},
		{"最多two items，最多three items", agent.ErrItemLimitText},
	} {
		_, err := NewPlanner().Resolve(agent.Input{Query: tc.query}, nil)
		if !errors.Is(err, tc.want) || !errors.Is(err, agent.ErrInvalidInput) || strings.Contains(err.Error(), tc.query) {
			t.Fatalf("Resolve(%q): got %v, want safe category %v", tc.query, err, tc.want)
		}
	}
}

func TestPlannerRejectsInvalidInheritedText(t *testing.T) {
	for _, query := range []string{"预算$500", "预算0.001元", "最多十一件"} {
		if _, err := NewPlanner().Resolve(agent.Input{Query: "再便携一点"}, []string{query}); !errors.Is(err, agent.ErrInvalidInput) {
			t.Fatalf("invalid history accepted: %q, %v", query, err)
		}
	}
}

func FuzzPlannerExactDecimalConstraints(f *testing.F) {
	for _, seed := range []struct {
		whole    uint64
		fraction uint16
		items    uint8
	}{
		{19, 990, 2}, {19, 999, 2}, {0, 1, 1}, {0, 10, 1}, {1_000_000_000, 0, 10}, {1_000_000_000, 1, 0},
	} {
		f.Add(seed.whole, seed.fraction, seed.items)
	}
	f.Fuzz(func(t *testing.T, whole uint64, fraction uint16, items uint8) {
		whole %= 1_000_000_002
		fraction %= 1000
		items %= 12
		query := fmt.Sprintf("预算%d.%03d元，最多%d件", whole, fraction, items)
		got, err := NewPlanner().Resolve(agent.Input{Query: query}, nil)
		cents := int64(whole)*100 + int64(fraction)/10
		invalid := cents == 0 || cents > agent.MaxBudgetCents || fraction%10 != 0 || items == 0 || items > 10
		if invalid {
			if !errors.Is(err, agent.ErrInvalidInput) {
				t.Fatalf("invalid %q: %+v, %v", query, got, err)
			}
		} else if err != nil || got.BudgetCents != cents || got.MaxItems != int32(items) {
			t.Fatalf("exact decimal %q: %+v, %v; want %d cents", query, got, err, cents)
		}
	})
}

func FuzzPlannerResolveText(f *testing.F) {
	for _, query := range []string{"预算3-5k最多两件", "预算-0.001元", "预算$500", "iPhone15 4K 65W", "预算1,000.50元", "预算3000或5000"} {
		f.Add(query)
	}
	f.Fuzz(func(t *testing.T, query string) {
		if len(query) > agent.MaxQueryRunes*4 {
			t.Skip()
		}
		planner := NewPlanner()
		got, err := planner.Resolve(agent.Input{Query: query}, nil)
		again, againErr := planner.Resolve(agent.Input{Query: query}, nil)
		if !reflect.DeepEqual(got, again) || (err == nil) != (againErr == nil) {
			t.Fatal("non-deterministic resolution")
		}
		if err != nil {
			if !errors.Is(err, agent.ErrInvalidInput) {
				t.Fatalf("unexpected error: %v", err)
			}
			return
		}
		if _, err := agent.NewConstraints(got); err != nil {
			t.Fatalf("unsafe resolved limits: %+v", got)
		}
		// 编排层把已解析值交给执行器后，再解析不得改变业务约束。
		execution, err := planner.Resolve(agent.Input{Query: query, BudgetCents: got.BudgetCents, MaxItems: got.MaxItems, PriorIntent: &got}, nil)
		if err != nil || !reflect.DeepEqual(got, execution) {
			t.Fatalf("execution changed constraints: %+v / %+v, %v", got, execution, err)
		}
	})
}

func TestPlannerConstraintPrecedence(t *testing.T) {
	prior := &agent.Intent{BudgetCents: 420000, MaxItems: 4, Keywords: []string{"手机"}}
	for _, tc := range []struct {
		name    string
		input   agent.Input
		history []string
		budget  int64
		items   int32
	}{
		{"explicit wins", agent.Input{Query: "预算$500，最多十一件", BudgetCents: 10000, MaxItems: 2, PriorIntent: prior}, nil, 10000, 2},
		{"explicit budget only", agent.Input{Query: "预算$500，最多两件", BudgetCents: 10000, PriorIntent: prior}, nil, 10000, 2},
		{"explicit count only", agent.Input{Query: "预算100元，最多十一件", MaxItems: 2, PriorIntent: prior}, nil, 10000, 2},
		{"text overrides prior", agent.Input{Query: "预算提高到5000，最多两件", PriorIntent: prior}, nil, 500000, 2},
		{"inherit prior", agent.Input{Query: "再便携一点", PriorIntent: prior}, nil, 420000, 4},
		{"inherit text", agent.Input{Query: "再便携一点"}, []string{"预算5000元，最多两件", "续航好一点"}, 500000, 2},
		{"structured ignores old ambiguous text", agent.Input{Query: "再便携一点", PriorIntent: prior}, []string{"预算$500，最多十一件"}, 420000, 4},
		{"explicit inheritance wording", agent.Input{Query: "预算保持不变，再便携一点", PriorIntent: prior}, nil, 420000, 4},
		{"current corrects old text", agent.Input{Query: "预算1000元，最多一件"}, []string{"预算$500，最多十一件"}, 100000, 1},
		{"latest history corrects older text", agent.Input{Query: "最多两件"}, []string{"预算$500", "预算1000元"}, 100000, 2},
		{"history fills only missing stored count", agent.Input{Query: "再便携一点", PriorIntent: &agent.Intent{BudgetCents: 420000}}, []string{"预算$500，最多两件"}, 420000, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewPlanner().Resolve(tc.input, tc.history)
			if err != nil || got.BudgetCents != tc.budget || got.MaxItems != tc.items {
				t.Fatalf("Resolve = %+v, %v; want budget=%d items=%d", got, err, tc.budget, tc.items)
			}
		})
	}
	for _, query := range []string{"预算-1元", "最多0件"} {
		if _, err := NewPlanner().Resolve(agent.Input{Query: query, PriorIntent: prior}, nil); !errors.Is(err, agent.ErrInvalidInput) {
			t.Fatalf("invalid current text inherited prior: %q, %v", query, err)
		}
	}
}
