package tools

import "context"

type SearchProductsReq struct {
	Query       string
	Keywords    []string
	BudgetCents int64
	MaxItems    int32
}

type ProductCandidate struct {
	ID         string
	Name       string
	Category   string
	Source     string
	PriceCents int64
	Stock      int64
	Sold       int64
	Tags       []string
}

type ProductProvider interface {
	Name() string
	SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error)
}
