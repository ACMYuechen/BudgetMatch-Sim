// Package recommend 提供基于预算与意图的商品推荐 Agent 实现。
// 该包负责解析用户查询意图、搜索候选商品，并从中选择最优商品组合。
package recommend

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
)

// Planner 意图解析器，负责从用户自然语言查询中提取预算、关键词和偏好。
type Planner struct{}

// NewPlanner 创建一个新的意图解析器实例。
func NewPlanner() *Planner {
	return &Planner{}
}

// Resolve 是唯一的意图解析入口。非法或有歧义的约束必须返回错误，不能变为默认值。
// 数值字段逐项遵循：当前显式值 > 当前文本 > 结构化状态 > 最近历史文本 > 默认值。
func (p *Planner) Resolve(input agent.Input, historyQueries []string) (agent.Intent, error) {
	if utf8.RuneCountInString(input.Query) > agent.MaxQueryRunes ||
		input.BudgetCents < 0 || input.BudgetCents > agent.MaxBudgetCents ||
		input.MaxItems < 0 || input.MaxItems > agent.MaxItems {
		return agent.Intent{}, fmt.Errorf("%w: explicit constraints outside allowed range", agent.ErrInvalidInput)
	}
	if prior := input.PriorIntent; prior != nil {
		if prior.BudgetCents < 0 || prior.BudgetCents > agent.MaxBudgetCents || prior.MaxItems < 0 || prior.MaxItems > agent.MaxItems {
			return agent.Intent{}, fmt.Errorf("%w: stored constraints outside allowed range", agent.ErrInvalidInput)
		}
	}
	current, err := parsePartial(input)
	if err != nil {
		return agent.Intent{}, err
	}
	if prior := input.PriorIntent; prior != nil {
		inheritLimits(&current, *prior)
	}
	// 从最近历史开始，仅解析尚未确定的字段。旧的歧义文本不能推翻已保存的明确约束。
	for i := len(historyQueries) - 1; i >= 0 && (current.BudgetCents == 0 || current.MaxItems == 0); i-- {
		query := historyQueries[i]
		if utf8.RuneCountInString(query) > agent.MaxQueryRunes {
			return agent.Intent{}, fmt.Errorf("%w: history query exceeds limit", agent.ErrInvalidInput)
		}
		partial, err := parsePartial(agent.Input{Query: query, BudgetCents: current.BudgetCents, MaxItems: current.MaxItems})
		if err != nil {
			return agent.Intent{}, err
		}
		inheritLimits(&current, partial)
	}
	// 商品关键词仍取最近有效值；偏好维持原有的按时间累积语义。
	var inherited agent.Intent
	for _, query := range historyQueries {
		query, _, err = filetools.ParseSaveRequest(query)
		if err != nil {
			return agent.Intent{}, fmt.Errorf("%w: invalid historical save directive", agent.ErrInvalidInput)
		}
		if keywords := extractKeywords(query); len(keywords) > 0 {
			inherited.Keywords = keywords
		}
		inherited.Preferences = mergeUnique(inherited.Preferences, extractPreferences(query))
	}
	if prior := input.PriorIntent; prior != nil {
		if len(prior.Keywords) > 0 {
			inherited.Keywords = append([]string(nil), prior.Keywords...)
		}
		inherited.Preferences = mergeUnique(inherited.Preferences, prior.Preferences)
	}
	if len(current.Keywords) == 0 {
		current.Keywords = inherited.Keywords
	}
	current.Preferences = mergeUnique(inherited.Preferences, current.Preferences)
	current = withIntentDefaults(current)
	if _, err := agent.NewConstraints(current); err != nil {
		return agent.Intent{}, err
	}
	return current, nil
}

func inheritLimits(current *agent.Intent, prior agent.Intent) {
	if current.BudgetCents == 0 {
		current.BudgetCents = prior.BudgetCents
	}
	if current.MaxItems == 0 {
		current.MaxItems = prior.MaxItems
	}
}

// parsePartial 不填默认值；只有未出现约束时才返回零值。
func parsePartial(input agent.Input) (agent.Intent, error) {
	query, _, err := filetools.ParseSaveRequest(input.Query)
	if err != nil {
		return agent.Intent{}, fmt.Errorf("%w: invalid save directive", agent.ErrInvalidInput)
	}
	input.Query = query
	current := agent.Intent{
		BudgetCents: input.BudgetCents, MaxItems: input.MaxItems,
		Keywords: extractKeywords(input.Query), Preferences: extractPreferences(input.Query),
	}
	query = normalizeConstraintText(input.Query)
	items, withoutQuantities, err := parseTextQuantities(query)
	if current.MaxItems == 0 {
		if err != nil {
			return agent.Intent{}, err
		}
		current.MaxItems = items
	}
	if current.BudgetCents == 0 {
		budget, err := parseTextBudget(withoutQuantities)
		if err != nil {
			return agent.Intent{}, err
		}
		current.BudgetCents = budget
	}
	return current, nil
}

func cloneIntent(intent agent.Intent) agent.Intent {
	intent.Keywords = append([]string(nil), intent.Keywords...)
	intent.Preferences = append([]string(nil), intent.Preferences...)
	return intent
}

// withIntentDefaults 为合并后仍缺失的约束填充默认值。
func withIntentDefaults(intent agent.Intent) agent.Intent {
	if intent.BudgetCents <= 0 {
		intent.BudgetCents = 300000
	}
	if intent.MaxItems <= 0 {
		intent.MaxItems = 3
	}
	if len(intent.Keywords) == 0 {
		intent.Keywords = []string{"study"}
	}
	return intent
}

// mergeUnique 按首次出现顺序合并字符串切片并去重。
func mergeUnique(base, additions []string) []string {
	seen := make(map[string]struct{}, len(base)+len(additions))
	merged := make([]string, 0, len(base)+len(additions))
	for _, values := range [][]string{base, additions} {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
}

// extractKeywords 从用户查询中提取商品类别关键词。
// 预定义关键词列表包含中英文常见商品与场景词。
func extractKeywords(query string) []string {
	lower := strings.ToLower(query)
	candidates := []string{
		"study", "office", "computer", "stationery",
		"smartphone", "phone", "headphones", "headphone", "earbuds", "earbud", "headset",
		"tablet", "monitor", "display", "keyboard", "mouse", "dormitory", "dorm", "commuting", "commute",
		"学习", "办公", "电脑", "文具", "手机", "耳机", "平板", "显示器", "键鼠", "键盘", "鼠标", "宿舍", "通勤",
	}
	var keywords []string
	for _, candidate := range candidates {
		if containsTerm(lower, candidate) {
			keywords = append(keywords, candidate)
		}
	}
	return keywords
}

// extractPreferences 从用户查询中提取用户偏好关键词。
// 支持识别性价比、便携、续航、轻薄、性能、品牌、耐用、静音等中英文偏好。
func extractPreferences(query string) []string {
	lower := strings.ToLower(query)
	var preferences []string
	candidates := []string{
		"value", "cheap", "portable", "battery life", "long battery", "lightweight", "slim",
		"performance", "brand", "durable", "durability", "quiet", "silent",
		"性价比", "便宜", "便携", "续航", "轻薄", "性能", "品牌", "耐用", "静音",
	}
	for _, candidate := range candidates {
		if containsTerm(lower, candidate) {
			preferences = append(preferences, candidate)
		}
	}
	return preferences
}

// containsTerm 判断文本是否包含候选词。
// 英文候选词按单词边界匹配，避免把 phone 从 headphones 等单词内部重复提取出来。
func containsTerm(text, term string) bool {
	searchFrom := 0
	for {
		offset := strings.Index(text[searchFrom:], term)
		if offset < 0 {
			return false
		}

		start := searchFrom + offset
		end := start + len(term)
		if !isASCIIWordByte(term[0]) ||
			(start == 0 || !isASCIIWordByte(text[start-1])) &&
				(end == len(text) || !isASCIIWordByte(text[end])) {
			return true
		}
		searchFrom = start + 1
	}
}

// isASCIIWordByte 判断字节是否属于英文单词或数字。
func isASCIIWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}
