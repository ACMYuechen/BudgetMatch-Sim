// Package tools 提供推荐 Agent 所需的外部工具与数据提供者抽象。
package tools

import "context"

// SearchProductsReq 表示搜索商品请求的参数。
type SearchProductsReq struct {
	Query       string   // Query 用户原始查询
	Keywords    []string // Keywords 搜索关键词列表
	BudgetCents int64    // BudgetCents 预算上限，单位为分
	MaxItems    int32    // MaxItems 期望返回的最大商品数量
}

// ProductCandidate 表示一个候选商品的信息。
type ProductCandidate struct {
	ID         string   // ID 商品唯一标识
	Name       string   // Name 商品名称
	Category   string   // Category 商品分类
	Source     string   // Source 商品来源
	PriceCents int64    // PriceCents 商品价格，单位为分
	Stock      int64    // Stock 库存数量
	Sold       int64    // Sold 历史销量
	Tags       []string // Tags 商品标签
}

// ProductProvider 定义商品数据提供者的接口。
type ProductProvider interface {
	// Name 返回提供者名称。
	Name() string
	// SearchProducts 根据请求搜索候选商品。
	SearchProducts(ctx context.Context, req SearchProductsReq) ([]ProductCandidate, error)
}
