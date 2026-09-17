package llm

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

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
	CandidateIds []string `json:"candidate_ids" jsonschema:"description=Product candidate Ids from search_products; empty means use all stored candidates"`
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, err := s.toolConstraints(args.BudgetCents, args.MaxItems)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" || utf8.RuneCountInString(args.Query) > agentcore.MaxQueryRunes || len(args.Keywords) > agentcore.MaxKeywords {
		return nil, fmt.Errorf("%w: tool query or keyword count outside allowed range", agentcore.ErrInvalidInput)
	}
	for _, keyword := range args.Keywords {
		if utf8.RuneCountInString(keyword) > agentcore.MaxKeywordRunes {
			return nil, fmt.Errorf("%w: tool keyword is too long", agentcore.ErrInvalidInput)
		}
	}
	products, err := s.provider.SearchProducts(ctx, tools.SearchProductsReq{
		Query:       args.Query,
		Keywords:    args.Keywords,
		BudgetCents: limits.BudgetCents,
		MaxItems:    limits.MaxItems,
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.storeCandidates(products)
	// 原始快照先入会话，使后来的缺货/非法快照也能使旧候选失效。
	products = agentcore.NormalizeCandidates(products)
	filtered := make([]tools.ProductCandidate, 0, len(products))
	for _, product := range products {
		if product.PriceCents <= limits.BudgetCents {
			filtered = append(filtered, product)
		}
	}
	return &searchResult{
		Products: filtered,
		Count:    len(filtered),
		Source:   s.provider.Name(),
	}, nil
}

// selectBundle 是 select_bundle 的执行逻辑：从缓存候选中挑选套装并写回 session。
func (s *session) selectBundle(ctx context.Context, args selectArgs) (*selectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, err := s.toolConstraints(args.BudgetCents, args.MaxItems)
	if err != nil {
		return nil, err
	}
	if len(args.CandidateIds) > agentcore.MaxCandidateIDs {
		return nil, fmt.Errorf("%w: too many candidate IDs", agentcore.ErrInvalidInput)
	}
	// 未知 ID 整次拒绝，而不是静默剔除后假装完成了模型请求。
	known := make(map[string]struct{})
	for _, candidate := range s.filterCandidates(nil) {
		known[candidate.Id] = struct{}{}
	}
	for _, id := range args.CandidateIds {
		if _, ok := known[id]; !ok {
			return nil, fmt.Errorf("%w: unknown candidate ID", agentcore.ErrInvalidInput)
		}
	}
	candidates := s.filterCandidates(args.CandidateIds)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no product candidates available; call %s first", toolSearchProducts)
	}
	items, total := s.selector.Select(candidates, agentcore.Intent{
		BudgetCents: limits.BudgetCents,
		MaxItems:    limits.MaxItems,
	})
	s.setBundle(items, total)
	return &selectResult{Items: items, TotalPriceCents: total}, nil
}

// toolConstraints 是搜索和选择的唯一约束入口，调整记录不包含原始工具参数。
func (s *session) toolConstraints(budget int64, count int32) (agentcore.Constraints, error) {
	limits, err := agentcore.NewConstraints(s.intent)
	if err != nil {
		return agentcore.Constraints{}, err
	}
	limits, adjusted, err := limits.Restrict(budget, count)
	if err != nil {
		return agentcore.Constraints{}, err
	}
	if adjusted {
		s.recordCall(agentcore.ToolCall{Name: "constraints.adjusted", Success: true, Detail: "tool limits capped by user constraints"})
	}
	return limits, nil
}
