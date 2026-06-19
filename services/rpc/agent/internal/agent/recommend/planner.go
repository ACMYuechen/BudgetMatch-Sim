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

// budgetPattern 用于从查询文本中提取预算金额的正则表达式。
// 支持匹配数字及单位：k、w、万、千、元、块（不区分大小写）。
var budgetPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(k|w|万|千|元|块)?`)

// parseBudget 从用户查询文本中解析预算金额（单位：分）。
// 匹配规则：提取所有数字及可选单位，按单位换算后，若金额大于等于 10 则转换为分返回。
func parseBudget(query string) int64 {
	matches := budgetPattern.FindAllStringSubmatch(query, -1)
	for _, match := range matches {
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		unit := strings.ToLower(match[2])
		switch unit {
		case "w", "万":
			value *= 10000
		case "k", "千":
			value *= 1000
		}
		if value >= 10 {
			return int64(value * 100)
		}
	}
	return 0
}

// extractKeywords 从用户查询中提取商品类别关键词。
// 预定义关键词列表包含中英文常见场景词（如学习、办公、电脑、文具、宿舍等）。
// 若未匹配到任何关键词，默认返回 "study"。
func extractKeywords(query string) []string {
	lower := strings.ToLower(query)
	candidates := []string{
		"study", "office", "computer", "stationery", "dorm",
		"学习", "办公", "电脑", "文具", "宿舍",
	}
	var keywords []string
	for _, candidate := range candidates {
		if strings.Contains(lower, candidate) {
			keywords = append(keywords, candidate)
		}
	}
	if len(keywords) == 0 {
		keywords = append(keywords, "study")
	}
	return keywords
}

// extractPreferences 从用户查询中提取用户偏好关键词。
// 支持识别的偏好包括：性价比、便宜、便携等（中英文）。
func extractPreferences(query string) []string {
	lower := strings.ToLower(query)
	var preferences []string
	for _, candidate := range []string{"value", "cheap", "portable", "性价比", "便宜", "便携"} {
		if strings.Contains(lower, candidate) {
			preferences = append(preferences, candidate)
		}
	}
	return preferences
}
