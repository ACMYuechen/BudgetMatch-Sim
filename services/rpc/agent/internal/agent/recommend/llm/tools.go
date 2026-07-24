package llm

import (
	"context"
	"fmt"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/filetools"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// 工具名称常量。
const (
	toolSearchProducts = "search_products"
	toolSelectBundle   = "select_bundle"
	toolReadFile       = "read_file"
	toolWriteFile      = "write_file"
)

// searchArgs 是 search_products 工具的入参，结构体标签直接驱动 Eino 生成的 JSON Schema。
type searchArgs struct {
	Query       string   `json:"query" jsonschema:"required,description=Original user shopping request"`
	Keywords    []string `json:"keywords" jsonschema:"description=Parsed shopping keywords"`
	BudgetCents int64    `json:"budget_cents" jsonschema:"description=Maximum budget in cents, use 0 when unknown"`
	MaxItems    int32    `json:"max_items" jsonschema:"description=Maximum number of bundle items"`
}

// searchResult 是 search_products 工具返回给模型的结果。
type searchResult struct {
	Products []tools.ProductCandidate `json:"products"`
	Count    int                      `json:"count"`
	Source   string                   `json:"source"`
}

// selectArgs 是 select_bundle 工具的入参。
type selectArgs struct {
	CandidateIDs []string `json:"candidate_ids" jsonschema:"description=Product candidate IDs from search_products; empty means use all stored candidates"`
	BudgetCents  int64    `json:"budget_cents" jsonschema:"description=Maximum budget in cents, use 0 when unknown"`
	MaxItems     int32    `json:"max_items" jsonschema:"description=Maximum number of bundle items"`
}

// selectResult 是 select_bundle 工具返回给模型的结果。
type selectResult struct {
	Items           []agentcore.BundleItem `json:"items"`
	TotalPriceCents int64                  `json:"total_price_cents"`
}

// readFileArgs 是 read_file 工具的入参。
type readFileArgs struct {
	Path string `json:"path" jsonschema:"required,description=Relative path inside the configured agent workspace"`
}

// readFileResult 是 read_file 工具返回给模型的结果。
type readFileResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// writeFileArgs 是 write_file 工具的入参。
type writeFileArgs struct {
	Path    string `json:"path" jsonschema:"required,description=Relative path inside the configured agent workspace"`
	Content string `json:"content" jsonschema:"required,description=Content to write to the file"`
}

// writeFileResult 是 write_file 工具返回给模型的结果。
type writeFileResult struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
}

// readFile 是 read_file 的执行逻辑：读取文件内容并返回。
func readFile(workspace *filetools.Workspace) func(context.Context, readFileArgs) (*readFileResult, error) {
	return func(ctx context.Context, args readFileArgs) (*readFileResult, error) {
		content, err := workspace.ReadFile(ctx, args.Path)
		if err != nil {
			return nil, err
		}
		return &readFileResult{Path: args.Path, Content: content}, nil
	}
}

// writeFile 返回受工作目录和文件后缀约束的 write_file 执行逻辑。
func writeFile(workspace *filetools.Workspace) func(context.Context, writeFileArgs) (*writeFileResult, error) {
	return func(ctx context.Context, args writeFileArgs) (*writeFileResult, error) {
		written, err := workspace.WriteFile(ctx, args.Path, args.Content)
		if err != nil {
			return nil, err
		}
		return &writeFileResult{Path: args.Path, BytesWritten: written}, nil
	}
}

// businessTools 把领域能力包装成 Eino 工具。
// 每个工具用 InferTool 从入参结构体推导 schema，handler 闭包持有 session 写入类型化结果，
// 再统一套上记录装饰器和错误处理器：工具出错时返回 JSON 让模型自行恢复，而不是中断整个 ReAct。
func businessTools(s *session, workspace *filetools.Workspace) ([]tool.BaseTool, error) {
	if workspace == nil {
		return nil, fmt.Errorf("file tools workspace is required")
	}

	search, err := utils.InferTool(
		toolSearchProducts,
		"Search product candidates by query, keywords, budget, and item limit. Use this before selecting a bundle.",
		s.searchProducts,
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", toolSearchProducts, err)
	}

	selectBundle, err := utils.InferTool(
		toolSelectBundle,
		"Select an MVP product bundle from candidate product IDs returned by search_products.",
		s.selectBundle,
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", toolSelectBundle, err)
	}

	readF, err := utils.InferTool(
		toolReadFile,
		"Read a size-limited file using a relative path inside the configured agent workspace.",
		readFile(workspace),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", toolReadFile, err)
	}

	writeF, err := utils.InferTool(
		toolWriteFile,
		"Write an allowed file type using a relative path inside the configured agent workspace.",
		writeFile(workspace),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", toolWriteFile, err)
	}

	return []tool.BaseTool{
		decorate(s, toolSearchProducts, search),
		decorate(s, toolSelectBundle, selectBundle),
		decorate(s, toolReadFile, readF),
		decorate(s, toolWriteFile, writeF),
	}, nil
}

// searchProducts 是 search_products 的执行逻辑：检索候选商品并缓存到 session。
func (s *session) searchProducts(ctx context.Context, args searchArgs) (*searchResult, error) {
	if args.MaxItems <= 0 {
		args.MaxItems = s.intent.MaxItems
	}
	products, err := s.provider.SearchProducts(ctx, tools.SearchProductsReq{
		Query:       args.Query,
		Keywords:    args.Keywords,
		BudgetCents: args.BudgetCents,
		MaxItems:    args.MaxItems,
	})
	if err != nil {
		return nil, err
	}
	s.storeCandidates(products)
	return &searchResult{
		Products: products,
		Count:    len(products),
		Source:   s.provider.Name(),
	}, nil
}

// selectBundle 是 select_bundle 的执行逻辑：从缓存候选中挑选套装并写回 session。
func (s *session) selectBundle(ctx context.Context, args selectArgs) (*selectResult, error) {
	_ = ctx
	if args.MaxItems <= 0 {
		args.MaxItems = s.intent.MaxItems
	}
	// 与 MaxItems 同等回落：模型按 schema 提示传 0 时用解析意图的预算兜底，
	// 否则预算约束会被静默绕过。
	if args.BudgetCents <= 0 {
		args.BudgetCents = s.intent.BudgetCents
	}
	candidates := s.filterCandidates(args.CandidateIDs)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no product candidates available; call %s first", toolSearchProducts)
	}
	items, total := s.selector.Select(candidates, agentcore.Intent{
		BudgetCents: args.BudgetCents,
		MaxItems:    args.MaxItems,
	})
	s.setBundle(items, total)
	return &selectResult{Items: items, TotalPriceCents: total}, nil
}
