package recommend

import (
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

// TestPlannerParseBudgetExpressions 验证 Planner 能解析中文、英文和缩写预算表达。
func TestPlannerParseBudgetExpressions(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int64
	}{
		{name: "中文以内", query: "预算5000以内买手机", want: 500000},
		{name: "中文左右", query: "想买3000元左右的平板", want: 300000},
		{name: "中文不超过", query: "手机预算不超过4500", want: 450000},
		{name: "区间缩写", query: "3-5k买通勤耳机", want: 500000},
		{name: "英文预算", query: "phone budget around 1.2k", want: 120000},
		{name: "英文上限", query: "headphones under 800", want: 80000},
		{name: "中英混合", query: "预算 under 2K，想要 lightweight 平板", want: 200000},
	}

	planner := NewPlanner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := planner.Parse(agent.Input{Query: tt.query})
			if intent.BudgetCents != tt.want {
				t.Fatalf("BudgetCents = %d, want %d", intent.BudgetCents, tt.want)
			}
		})
	}
}

// TestPlannerSpecifiedScenarios 验证指定查询能正确解析预算、关键词并忽略型号数字。
func TestPlannerSpecifiedScenarios(t *testing.T) {
	planner := NewPlanner()

	t.Run("区间预算和通勤耳机关键词", func(t *testing.T) {
		intent := planner.Parse(agent.Input{Query: "预算 3-5k 买通勤耳机"})
		if intent.BudgetCents != 500000 {
			t.Fatalf("BudgetCents = %d, want 500000", intent.BudgetCents)
		}
		assertContainsAll(t, intent.Keywords, []string{"通勤", "耳机"})
	})

	t.Run("不超过五千", func(t *testing.T) {
		intent := planner.Parse(agent.Input{Query: "不超过 5000 买办公电脑"})
		if intent.BudgetCents != 500000 {
			t.Fatalf("BudgetCents = %d, want 500000", intent.BudgetCents)
		}
		assertContainsAll(t, intent.Keywords, []string{"办公", "电脑"})
	})

	t.Run("忽略手机型号数字", func(t *testing.T) {
		intent := planner.Parse(agent.Input{Query: "买 iPhone 15 配件"})
		if intent.BudgetCents != 300000 {
			t.Fatalf("BudgetCents = %d, want default 300000", intent.BudgetCents)
		}
	})
}

// TestPlannerKeepsExplicitAndDefaultBudget 验证结构化预算优先，并保持未识别预算时的默认值。
func TestPlannerKeepsExplicitAndDefaultBudget(t *testing.T) {
	planner := NewPlanner()

	explicit := planner.Parse(agent.Input{Query: "预算不超过5000", BudgetCents: 123400})
	if explicit.BudgetCents != 123400 {
		t.Fatalf("explicit BudgetCents = %d, want 123400", explicit.BudgetCents)
	}

	queries := []string{
		"买2个iPhone 15 Pro Max手机，搭配4K显示器和65W充电器",
		"最多3件耳机，型号选Redmi K70",
		"不超过15件商品",
		"need 2 headphones and an iPhone 16",
	}
	for _, query := range queries {
		intent := planner.Parse(agent.Input{Query: query})
		if intent.BudgetCents != 300000 {
			t.Errorf("query %q: BudgetCents = %d, want default 300000", query, intent.BudgetCents)
		}
	}
}

// TestPlannerPrefersExplicitBudget 验证显式预算不会被后续商品价格区间覆盖。
func TestPlannerPrefersExplicitBudget(t *testing.T) {
	intent := NewPlanner().Parse(agent.Input{Query: "预算1000元，耳机价格300-500元"})
	if intent.BudgetCents != 100000 {
		t.Fatalf("BudgetCents = %d, want 100000", intent.BudgetCents)
	}
}

// TestPlannerParseWithHistory 验证当前轮缺失的商品上下文会从历史继承，而当前轮新约束优先。
func TestPlannerParseWithHistory(t *testing.T) {
	planner := NewPlanner()

	t.Run("预算追问继承商品上下文", func(t *testing.T) {
		intent := planner.ParseWithHistory(agent.Input{Query: "预算提高到5000"}, []string{
			"预算3000买通勤耳机",
			"希望续航好一点",
		})
		if intent.BudgetCents != 500000 {
			t.Fatalf("BudgetCents = %d, want 500000", intent.BudgetCents)
		}
		assertContainsAll(t, intent.Keywords, []string{"通勤", "耳机"})
		assertContainsAll(t, intent.Preferences, []string{"续航"})
	})

	t.Run("当前商品类别覆盖历史类别", func(t *testing.T) {
		intent := planner.ParseWithHistory(agent.Input{Query: "换成平板，预算2000元"}, []string{
			"预算5000买手机",
		})
		if intent.BudgetCents != 200000 {
			t.Fatalf("BudgetCents = %d, want 200000", intent.BudgetCents)
		}
		if !containsString(intent.Keywords, "平板") || containsString(intent.Keywords, "手机") {
			t.Fatalf("current keywords should replace historical category, got %v", intent.Keywords)
		}
	})
}

// TestPlannerExtractKeywords 验证常见商品类别与使用场景的中英文关键词识别。
func TestPlannerExtractKeywords(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "中文关键词",
			query: "手机、耳机、平板、显示器和键鼠，适合宿舍与通勤",
			want:  []string{"手机", "耳机", "平板", "显示器", "键鼠", "宿舍", "通勤"},
		},
		{
			name:  "英文关键词",
			query: "phone, headphones, tablet, monitor, keyboard and mouse for dorm commute",
			want:  []string{"phone", "headphones", "tablet", "monitor", "keyboard", "mouse", "dorm", "commute"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertContainsAll(t, extractKeywords(tt.query), tt.want)
		})
	}

	keywords := extractKeywords("headphones for commute")
	if containsString(keywords, "phone") {
		t.Fatalf("headphones 不应额外识别为 phone，实际结果：%v", keywords)
	}
}

// TestPlannerExtractPreferences 验证续航、轻薄、性能、品牌、耐用和静音偏好的中英文识别。
func TestPlannerExtractPreferences(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "中文偏好",
			query: "要续航好、轻薄、性能强，重视品牌，耐用且静音",
			want:  []string{"续航", "轻薄", "性能", "品牌", "耐用", "静音"},
		},
		{
			name:  "英文偏好",
			query: "battery life, lightweight, performance, brand, durable and quiet",
			want:  []string{"battery life", "lightweight", "performance", "brand", "durable", "quiet"},
		},
		{
			name:  "混合偏好",
			query: "portable 平板，续航要好并且 silent",
			want:  []string{"portable", "续航", "silent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertContainsAll(t, extractPreferences(tt.query), tt.want)
		})
	}
}

// assertContainsAll 验证实际切片包含全部期望值。
func assertContainsAll(t *testing.T, actual, expected []string) {
	t.Helper()
	for _, value := range expected {
		if !containsString(actual, value) {
			t.Errorf("结果 %v 缺少 %q", actual, value)
		}
	}
}

// containsString 判断字符串切片是否包含指定值。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
