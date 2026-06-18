package tools

import (
	"context"
	"strings"
)

type MockProductProvider struct {
	products []ProductCandidate
}

func NewMockProductProvider() *MockProductProvider {
	return &MockProductProvider{
		products: []ProductCandidate{
			{ID: "mock_keyboard_001", Name: "Entry Mechanical Keyboard", Category: "study", Source: "mock", PriceCents: 25900, Stock: 42, Sold: 1800, Tags: []string{"office", "study", "value"}},
			{ID: "mock_mouse_001", Name: "Wireless Silent Mouse", Category: "study", Source: "mock", PriceCents: 9900, Stock: 120, Sold: 4300, Tags: []string{"office", "study", "portable"}},
			{ID: "mock_lamp_001", Name: "Eye Protection Desk Lamp", Category: "study", Source: "mock", PriceCents: 16900, Stock: 64, Sold: 2100, Tags: []string{"study", "dorm", "health"}},
			{ID: "mock_monitor_001", Name: "24 Inch IPS Monitor", Category: "computer", Source: "mock", PriceCents: 69900, Stock: 18, Sold: 860, Tags: []string{"office", "computer", "screen"}},
			{ID: "mock_headset_001", Name: "Noise Cancelling Headset", Category: "computer", Source: "mock", PriceCents: 32900, Stock: 35, Sold: 980, Tags: []string{"meeting", "computer", "audio"}},
			{ID: "mock_notebook_001", Name: "Thick Grid Notebook Set", Category: "stationery", Source: "mock", PriceCents: 4900, Stock: 300, Sold: 9200, Tags: []string{"study", "stationery", "value"}},
			{ID: "mock_pen_001", Name: "Gel Pen Value Pack", Category: "stationery", Source: "mock", PriceCents: 3900, Stock: 500, Sold: 12000, Tags: []string{"study", "stationery", "value"}},
		},
	}
}

func (p *MockProductProvider) Name() string {
	return "mock.product_provider"
}

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

func matchesAny(product ProductCandidate, keywords []string) bool {
	target := strings.ToLower(product.Name + " " + product.Category + " " + strings.Join(product.Tags, " "))
	for _, keyword := range keywords {
		if strings.Contains(target, keyword) {
			return true
		}
	}
	return false
}
