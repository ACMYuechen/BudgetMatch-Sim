package recommend

import (
	"regexp"
	"strconv"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

type Planner struct{}

func NewPlanner() *Planner {
	return &Planner{}
}

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

var budgetPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(k|w|万|千|元|块)?`)

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
