// Package toolkit 提供推荐 Agent 的工具执行层，包括商品搜索、组合选择及 MCP 工具调用。
package toolkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

// 工具名称常量。
const (
	// ToolSearchProducts 是商品搜索工具的名称。
	ToolSearchProducts = "search_products"
	// ToolSelectBundle 是商品组合选择工具的名称。
	ToolSelectBundle = "select_bundle"
	// ToolMCPCallTool 是 MCP 外部工具调用工具的名称。
	ToolMCPCallTool = "mcp_call_tool"
)

// Executor 是推荐工具的统一执行器，根据工具名称路由到对应的执行逻辑。
type Executor struct {
	provider *candidateStore          // 商品候选缓存（封装了 ProductProvider）
	selector *selector.BundleSelector // 商品组合选择器
	mcpCfg   mcp.Config               // MCP 配置
}

// SearchProductsArgs 是 search_products 工具的参数结构。
type SearchProductsArgs struct {
	Query       string   `json:"query"`        // 用户原始购物请求
	Keywords    []string `json:"keywords"`     // 解析后的购物关键词
	BudgetCents int64    `json:"budget_cents"` // 最大预算（单位：分），未知时传 0
	MaxItems    int32    `json:"max_items"`    // 组合中最多包含的商品数量
}

// SearchProductsResult 是 search_products 工具的返回结构。
type SearchProductsResult struct {
	Products []tools.ProductCandidate `json:"products"` // 搜索到的商品候选列表
	Count    int                      `json:"count"`    // 候选数量
	Source   string                   `json:"source"`   // 数据来源名称
}

// SelectBundleArgs 是 select_bundle 工具的参数结构。
type SelectBundleArgs struct {
	CandidateIDs []string `json:"candidate_ids"` // 候选商品 ID 列表，空表示使用全部缓存候选
	BudgetCents  int64    `json:"budget_cents"`  // 最大预算（单位：分），未知时传 0
	MaxItems     int32    `json:"max_items"`     // 组合中最多包含的商品数量
}

// SelectBundleResult 是 select_bundle 工具的返回结构。
type SelectBundleResult struct {
	Items           []agentcore.BundleItem `json:"items"`             // 选中的商品组合
	TotalPriceCents int64                  `json:"total_price_cents"` // 组合总价（单位：分）
}

// MCPCallToolArgs 是 mcp_call_tool 工具的参数结构。
type MCPCallToolArgs struct {
	Name      string         `json:"name"`      // MCP 工具名称
	Arguments map[string]any `json:"arguments"` // 传给 MCP 工具的 JSON 参数
}

// MCPCallToolResult 是 mcp_call_tool 工具的返回结构。
type MCPCallToolResult struct {
	Name    string            `json:"name"`     // 调用的 MCP 工具名称
	Content []mcp.ToolContent `json:"content"`  // MCP 工具返回的内容列表
	IsError bool              `json:"is_error"` // 是否发生错误
}

// Result 是工具执行的统一返回结构，包含名称、JSON 序列化结果及原始值。
type Result struct {
	Name   string          `json:"name"` // 工具名称
	JSON   json.RawMessage `json:"json"` // JSON 序列化后的结果
	Result any             `json:"-"`    // 原始结果对象（不参与 JSON 序列化）
}

// NewExecutor 创建一个新的 Executor 实例，使用提供的 ProductProvider 和 BundleSelector。
func NewExecutor(provider tools.ProductProvider, selector *selector.BundleSelector) *Executor {
	return &Executor{
		provider: newCandidateStore(provider),
		selector: selector,
	}
}

// WithMCP 为 Executor 设置 MCP 配置，支持链式调用。
func (e *Executor) WithMCP(c mcp.Config) *Executor {
	e.mcpCfg = c
	return e
}

// Execute 根据工具名称路由到对应的执行逻辑，返回统一的 Result。
func (e *Executor) Execute(ctx context.Context, name string, rawArgs json.RawMessage) (*Result, error) {
	switch name {
	case ToolSearchProducts:
		return e.searchProducts(ctx, rawArgs)
	case ToolSelectBundle:
		return e.selectBundle(ctx, rawArgs)
	case ToolMCPCallTool:
		return e.callMCPTool(ctx, rawArgs)
	default:
		return nil, fmt.Errorf("unknown recommend tool %q", name)
	}
}

// searchProducts 执行商品搜索：解析参数、调用 provider 搜索、缓存结果并返回。
func (e *Executor) searchProducts(ctx context.Context, rawArgs json.RawMessage) (*Result, error) {
	var args SearchProductsArgs
	if err := decodeArgs(rawArgs, &args); err != nil {
		return nil, err
	}
	if args.MaxItems <= 0 {
		args.MaxItems = 3
	}

	products, err := e.provider.SearchProducts(ctx, tools.SearchProductsReq{
		Query:       args.Query,
		Keywords:    args.Keywords,
		BudgetCents: args.BudgetCents,
		MaxItems:    args.MaxItems,
	})
	if err != nil {
		return nil, err
	}

	result := SearchProductsResult{
		Products: products,
		Count:    len(products),
		Source:   e.provider.Name(),
	}
	return newResult(ToolSearchProducts, result)
}

// selectBundle 执行商品组合选择：解析参数、从缓存中筛选候选、调用选择器生成组合。
func (e *Executor) selectBundle(ctx context.Context, rawArgs json.RawMessage) (*Result, error) {
	_ = ctx

	var args SelectBundleArgs
	if err := decodeArgs(rawArgs, &args); err != nil {
		return nil, err
	}
	if args.MaxItems <= 0 {
		args.MaxItems = 3
	}

	candidates := e.provider.Filter(args.CandidateIDs)
	if len(candidates) == 0 {
		return nil, errors.New("no product candidates available for select_bundle")
	}

	items, total := e.selector.Select(candidates, agentcore.Intent{
		BudgetCents: args.BudgetCents,
		MaxItems:    args.MaxItems,
	})
	return newResult(ToolSelectBundle, SelectBundleResult{
		Items:           items,
		TotalPriceCents: total,
	})
}

// callMCPTool 执行 MCP 工具调用：检查 MCP 是否启用、解析参数、启动 MCP 客户端并调用工具。
func (e *Executor) callMCPTool(ctx context.Context, rawArgs json.RawMessage) (*Result, error) {
	if !e.mcpCfg.Ready() {
		return nil, errors.New("mcp is not enabled")
	}

	var args MCPCallToolArgs
	if err := decodeArgs(rawArgs, &args); err != nil {
		return nil, err
	}
	if args.Name == "" {
		return nil, errors.New("mcp tool name is required")
	}
	if args.Arguments == nil {
		args.Arguments = map[string]any{}
	}

	client := mcp.NewClient(e.mcpCfg)
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	defer client.Close()

	toolResult, err := client.CallTool(ctx, args.Name, args.Arguments)
	if err != nil {
		return nil, err
	}

	return newResult(ToolMCPCallTool, MCPCallToolResult{
		Name:    args.Name,
		Content: toolResult.Content,
		IsError: toolResult.IsError,
	})
}

// decodeArgs 将 JSON 原始参数反序列化到目标结构；若 raw 为空则视为空对象 {}。
func decodeArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode recommend tool args: %w", err)
	}
	return nil
}

// newResult 将工具执行结果封装为统一的 Result 结构，包含 JSON 序列化后的数据。
func newResult(name string, value any) (*Result, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s result: %w", name, err)
	}
	return &Result{
		Name:   name,
		JSON:   data,
		Result: value,
	}, nil
}
