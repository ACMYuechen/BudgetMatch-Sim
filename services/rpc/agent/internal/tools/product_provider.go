// Package tools 提供推荐 Agent 所需的外部工具与数据提供者抽象。
package tools

import (
	"context"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

// SearchProductsReq 表示搜索商品请求的参数。
type SearchProductsReq struct {
	Query       string   // Query 用户原始查询
	Keywords    []string // Keywords 搜索关键词列表
	BudgetCents int64    // BudgetCents 预算上限，单位为分
	MaxItems    int32    // MaxItems 期望返回的最大商品数量
}

// ProductCandidate 表示一个候选商品的信息。
type ProductCandidate = agent.ProductCandidate

// ProductProvider 定义商品数据提供者的接口。
type ProductProvider interface {
	// Name 返回提供者名称。
	Name() string
	// SearchProducts 根据请求搜索候选商品。
	SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error)
}

// RankedProductProvider returns a bounded RAW keyword window in provider order.
// Invalid price/stock facts are retained so fusion can invalidate older copies.
// This is internal-only; model tool arguments cannot raise the window size.
type RankedProductProvider interface {
	SearchRankedProducts(context.Context, SearchProductsReq, int) ([]ProductCandidate, error)
}

// ProductSearch is per request. Providers must not store a mutable "last trace".
type ProductSearch struct {
	Candidates []ProductCandidate
	Calls      []agent.ToolCall
}

type TracedProductProvider interface {
	SearchProductsWithTrace(context.Context, SearchProductsReq) (ProductSearch, error)
}

func SearchWithTrace(ctx context.Context, provider ProductProvider, req SearchProductsReq) (ProductSearch, error) {
	if traced, ok := provider.(TracedProductProvider); ok {
		return traced.SearchProductsWithTrace(ctx, req)
	}
	candidates, err := provider.SearchProducts(ctx, req)
	return ProductSearch{Candidates: candidates}, err
}
