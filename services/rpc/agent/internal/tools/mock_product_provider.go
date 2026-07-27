// Package tools 提供推荐 Agent 所需的外部工具与数据提供者实现。
package tools

import (
	"context"
	"strings"
)

// MockProductProvider 是 ProductProvider 的内存模拟实现，内置一组示例商品。
type MockProductProvider struct {
	products []ProductCandidate // products 模拟商品列表
}

// NewMockProductProvider 创建内置示例商品的 MockProductProvider 实例。
func NewMockProductProvider() *MockProductProvider {
	return &MockProductProvider{
		products: []ProductCandidate{
			{Id: "mock_keyboard_001", Name: "Entry Mechanical Keyboard", Category: "study", Source: "mock", PriceCents: 25900, Stock: 42, Sold: 1800, Tags: []string{"office", "study", "value"}},
			{Id: "mock_mouse_001", Name: "Wireless Silent Mouse", Category: "study", Source: "mock", PriceCents: 9900, Stock: 120, Sold: 4300, Tags: []string{"office", "study", "portable"}},
			{Id: "mock_lamp_001", Name: "Eye Protection Desk Lamp", Category: "study", Source: "mock", PriceCents: 16900, Stock: 64, Sold: 2100, Tags: []string{"study", "dorm", "health"}},
			{Id: "mock_monitor_001", Name: "24 Inch IPS Monitor", Category: "computer", Source: "mock", PriceCents: 69900, Stock: 18, Sold: 860, Tags: []string{"office", "computer", "screen"}},
			{Id: "mock_headset_001", Name: "Noise Cancelling Headset", Category: "computer", Source: "mock", PriceCents: 32900, Stock: 35, Sold: 980, Tags: []string{"meeting", "computer", "audio"}},
			{Id: "mock_notebook_001", Name: "Thick Grid Notebook Set", Category: "stationery", Source: "mock", PriceCents: 4900, Stock: 300, Sold: 9200, Tags: []string{"study", "stationery", "value"}},
			{Id: "mock_pen_001", Name: "Gel Pen Value Pack", Category: "stationery", Source: "mock", PriceCents: 3900, Stock: 500, Sold: 12000, Tags: []string{"study", "stationery", "value"}},
		},
	}
}

// Name 返回该提供者的标识名称。
func (p *MockProductProvider) Name() string {
	return "mock.product_provider"
}

// SearchProducts 根据查询、关键词和预算筛选候选商品；若无匹配则返回预算内全部商品。
func (p *MockProductProvider) SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error) {
	_ = ctx

	keywords := normalizeKeywords(req.Keywords)
	if len(keywords) == 0 {
		keywords = normalizeKeywords([]string{req.Query})
	}

	var out []ProductCandidate
	for _, product := range p.products {
		if product.Stock <= 0 {
			continue
		}
		if req.BudgetCents > 0 && product.PriceCents > req.BudgetCents {
			continue
		}
		if len(keywords) > 0 && !matchesAny(product, keywords) {
			continue
		}
		out = append(out, product)
	}

	if len(out) == 0 {
		for _, product := range p.products {
			if product.Stock > 0 && (req.BudgetCents <= 0 || product.PriceCents <= req.BudgetCents) {
				out = append(out, product)
			}
		}
	}

	return out, nil
}

// normalizeKeywords 将输入文本归一化为小写、去重后的关键词列表。
func normalizeKeywords(values []string) []string {
	seen := make(map[string]struct{})
	var keywords []string
	for _, value := range values {
		for _, token := range strings.Fields(strings.ToLower(value)) {
			token = strings.Trim(token, " ,.;:!?，。；：！？")
			if token == "" {
				continue
			}
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			keywords = append(keywords, token)
		}
	}
	return keywords
}

// matchesAny 判断商品名称、分类或标签中是否包含任意关键词。
func matchesAny(product ProductCandidate, keywords []string) bool {
	target := strings.ToLower(product.Name + " " + product.Category + " " + strings.Join(product.Tags, " "))
	for _, keyword := range keywords {
		if strings.Contains(target, keyword) {
			return true
		}
	}
	return false
}
