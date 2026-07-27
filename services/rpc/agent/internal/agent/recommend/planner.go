// Package recommend 提供基于预算与意图的商品推荐 Agent 实现。
// 该包负责解析用户查询意图、搜索候选商品，并从中选择最优商品组合。
package recommend

import (
	"regexp"
	"strconv"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

// Planner 意图解析器，负责从用户自然语言查询中提取预算、关键词和偏好。
type Planner struct{}

// NewPlanner 创建一个新的意图解析器实例。
func NewPlanner() *Planner {
	return &Planner{}
}

// Parse 解析用户输入，提取推荐意图（预算、最大商品数、关键词、偏好）。
// 解析逻辑：
//  1. 若输入中未指定预算，则尝试从查询文本中通过正则提取预算金额；
//  2. 若仍未提取到预算，则使用默认预算 300000（即 3000 元）；
//  3. 若未指定最大商品数，则使用默认值 3。
func (p *Planner) Parse(input agent.Input) agent.Intent {
	budget := input.BudgetCents
	if budget <= 0 {
		budget = parseBudget(input.Query)
	}
	if budget <= 0 {
		budget = 300000
	}

	maxItems := input.MaxItems
	if maxItems <= 0 {
		maxItems = 3
	}

	return agent.Intent{
		BudgetCents: budget,
		MaxItems:    maxItems,
		Keywords:    extractKeywords(input.Query),
		Preferences: extractPreferences(input.Query),
	}
}

const budgetUnitExpression = `k|w|万|千|人民币|块钱|元|块|rmb|cny|yuan|usd|dollars?`

var (
	// quantityPattern 剔除带明确计数单位的商品数量，避免将数量上限解析为预算。
	quantityPattern = regexp.MustCompile(`(?i)\d+(?:\.\d+)?\s*(?:个|件|台|部|套|副|只|items?\b|pieces?\b|units?\b)`)
	// budgetRangePattern 匹配“3-5k”“预算 3000 到 5000”等预算区间。
	budgetRangePattern = regexp.MustCompile(
		`(?i)(预算(?:为|是|在|约|大概|控制在|不超过|不要超过|不超|最多)?|budget(?:\s+(?:is|around|about|under|below|within|up\s+to|max(?:imum)?))?)?` +
			`\s*[:：]?\s*([¥￥$])?\s*(\d+(?:\.\d+)?)\s*(` + budgetUnitExpression + `)?` +
			`\s*(?:[-~～—–]|至|到)\s*([¥￥$])?\s*(\d+(?:\.\d+)?)\s*(` + budgetUnitExpression + `)?` +
			`\s*(以内|左右|上下|以下|之内|封顶|or\s+less|or\s+so)?`,
	)

	// budgetPrefixPattern 匹配预算、不超过、under、around 等位于金额前的限定语。
	budgetPrefixPattern = regexp.MustCompile(
		`(?i)(?:` +
			`预算(?:为|是|在|约|大概|控制在|不超过|不要超过|不超|最多|上限(?:为|是)?)?` +
			`|(?:不超过|不要超过|不超|最多|上限(?:为|是)?)\s*(?:预算)?` +
			`|budget(?:\s+(?:is|around|about|under|below|within|up\s+to|max(?:imum)?))?` +
			`|(?:under|below|within|around|about|up\s+to|no\s+more\s+than|less\s+than)(?:\s+budget)?` +
			`|max(?:imum)?\s+budget` +
			`)\s*[:：]?\s*([¥￥$])?\s*(\d+(?:\.\d+)?)\s*(` + budgetUnitExpression + `)?`,
	)

	// budgetSuffixPattern 匹配“5000 以内”“3000 元左右”等金额在前的表达。
	budgetSuffixPattern = regexp.MustCompile(
		`(?i)([¥￥$])?\s*(\d+(?:\.\d+)?)\s*(` + budgetUnitExpression + `)?` +
			`\s*(?:以内|左右|上下|以下|之内|封顶|预算|or\s+less|or\s+so)`,
	)

	// currencySymbolPattern 匹配带有明确货币符号的金额。
	currencySymbolPattern = regexp.MustCompile(
		`(?i)([¥￥$])\s*(\d+(?:\.\d+)?)\s*(` + budgetUnitExpression + `)?`,
	)

	// currencyUnitPattern 只匹配明确的货币单位，避免把 4K 分辨率或 65W 功率识别为预算。
	currencyUnitPattern = regexp.MustCompile(
		`(?i)(\d+(?:\.\d+)?)\s*(万|千|人民币|块钱|元|块|rmb|cny|yuan|usd|dollars?)`,
	)
)

// parseBudget 从用户查询文本中解析预算金额（单位：分）。
// 显式预算限定语优先于普通金额；区间预算取上限。没有预算语义的裸数字不会参与解析。
func parseBudget(query string) int64 {
	query = quantityPattern.ReplaceAllString(query, "")
	if budget := parseBudgetRange(query, true); budget > 0 {
		return budget
	}
	if budget := parseMatchedBudget(budgetPrefixPattern.FindAllStringSubmatch(query, -1), 2, 3); budget > 0 {
		return budget
	}
	if budget := parseMatchedBudget(budgetSuffixPattern.FindAllStringSubmatch(query, -1), 2, 3); budget > 0 {
		return budget
	}
	if budget := parseBudgetRange(query, false); budget > 0 {
		return budget
	}
	if budget := parseMatchedBudget(currencySymbolPattern.FindAllStringSubmatch(query, -1), 2, 3); budget > 0 {
		return budget
	}
	return parseMatchedBudget(currencyUnitPattern.FindAllStringSubmatch(query, -1), 1, 2)
}

// parseBudgetRange 解析预算区间并返回区间上限。
// requireMarker 为 true 时仅接受带预算限定语的区间，确保显式预算优先于商品价格区间。
func parseBudgetRange(query string, requireMarker bool) int64 {
	for _, match := range budgetRangePattern.FindAllStringSubmatch(query, -1) {
		hasMarker := match[1] != "" || match[8] != ""
		hasMoneySignal := hasMarker || match[2] != "" || match[4] != "" || match[5] != "" || match[7] != ""
		if (requireMarker && !hasMarker) || (!requireMarker && !hasMoneySignal) {
			continue
		}

		firstUnit, secondUnit := match[4], match[7]
		if firstUnit == "" {
			firstUnit = secondUnit
		}
		if secondUnit == "" {
			secondUnit = firstUnit
		}

		first, firstOK := budgetAmountToCents(match[3], firstUnit)
		second, secondOK := budgetAmountToCents(match[6], secondUnit)
		if !firstOK || !secondOK {
			continue
		}
		if first > second {
			return first
		}
		return second
	}
	return 0
}

// parseMatchedBudget 从正则匹配结果中读取金额和单位。
func parseMatchedBudget(matches [][]string, amountIndex, unitIndex int) int64 {
	for _, match := range matches {
		if budget, ok := budgetAmountToCents(match[amountIndex], match[unitIndex]); ok {
			return budget
		}
	}
	return 0
}

// budgetAmountToCents 将金额文本按单位换算为分。
func budgetAmountToCents(amount, unit string) (int64, bool) {
	value, err := strconv.ParseFloat(amount, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	if unit == "" && value < 10 {
		return 0, false
	}

	switch strings.ToLower(unit) {
	case "w", "万":
		value *= 10000
	case "k", "千":
		value *= 1000
	}
	return int64(value * 100), true
}

// extractKeywords 从用户查询中提取商品类别关键词。
// 预定义关键词列表包含中英文常见商品与场景词。
// 若未匹配到任何关键词，默认返回 "study"。
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
	if len(keywords) == 0 {
		keywords = append(keywords, "study")
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
