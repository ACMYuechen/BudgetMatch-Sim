// Package eino 提供基于 Eino 框架的 LLM 模型封装、Prompt 构建、工具定义与 ReAct Agent 运行器。
package eino

import (
	"context"
	"encoding/json"

	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// executorTool 是 Eino 工具接口的适配器，将 toolkit.Executor 中的工具执行逻辑封装为 Eino BaseTool。
type executorTool struct {
	name     string            // 工具名称
	info     *schema.ToolInfo  // 工具元信息（描述、参数等）
	executor *toolkit.Executor // 底层工具执行器
}

// NewTools 根据 toolkit.Executor 创建 Eino 工具列表，包含 search_products、select_bundle 和 mcp_call_tool，
// 并为每个工具包装统一的错误处理，确保执行失败时返回标准 JSON 错误格式。
func NewTools(executor *toolkit.Executor) []tool.BaseTool {
	tools := []tool.BaseTool{
		&executorTool{
			name:     toolkit.ToolSearchProducts,
			info:     searchProductsInfo(),
			executor: executor,
		},
		&executorTool{
			name:     toolkit.ToolSelectBundle,
			info:     selectBundleInfo(),
			executor: executor,
		},
		&executorTool{
			name:     toolkit.ToolMCPCallTool,
			info:     mcpCallToolInfo(),
			executor: executor,
		},
	}

	for i := range tools {
		tools[i] = utils.WrapToolWithErrorHandler(tools[i], toolErrorJSON)
	}
	return tools
}

// Info 返回工具的元信息描述。
func (t *executorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return t.info, nil
}

// InvokableRun 执行工具调用：将 JSON 参数传递给 toolkit.Executor 执行，并返回 JSON 结果字符串。
func (t *executorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	result, err := t.executor.Execute(ctx, t.name, json.RawMessage(argumentsInJSON))
	if err != nil {
		return "", err
	}
	return string(result.JSON), nil
}

// searchProductsInfo 返回 search_products 工具的元信息定义。
func searchProductsInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: toolkit.ToolSearchProducts,
		Desc: "Search product candidates by query, keywords, budget, and item limit. Use this before selecting a bundle.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "Original user shopping request.",
				Required: true,
			},
			"keywords": {
				Type:     schema.Array,
				Desc:     "Parsed shopping keywords.",
				ElemInfo: &schema.ParameterInfo{Type: schema.String},
				Required: true,
			},
			"budget_cents": {
				Type:     schema.Integer,
				Desc:     "Maximum budget in cents. Use 0 when unknown.",
				Required: true,
			},
			"max_items": {
				Type:     schema.Integer,
				Desc:     "Maximum number of bundle items.",
				Required: true,
			},
		}),
	}
}

// selectBundleInfo 返回 select_bundle 工具的元信息定义。
func selectBundleInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: toolkit.ToolSelectBundle,
		Desc: "Select an MVP product bundle from candidate product IDs returned by search_products.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"candidate_ids": {
				Type:     schema.Array,
				Desc:     "Product candidate IDs to compare. Empty means use all stored candidates from search_products.",
				ElemInfo: &schema.ParameterInfo{Type: schema.String},
				Required: true,
			},
			"budget_cents": {
				Type:     schema.Integer,
				Desc:     "Maximum budget in cents. Use 0 when unknown.",
				Required: true,
			},
			"max_items": {
				Type:     schema.Integer,
				Desc:     "Maximum number of bundle items.",
				Required: true,
			},
		}),
	}
}

// mcpCallToolInfo 返回 mcp_call_tool 工具的元信息定义。
func mcpCallToolInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: toolkit.ToolMCPCallTool,
		Desc: "Call an enabled MCP server tool by name with JSON arguments. Use only when the user's goal benefits from an external MCP tool.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {
				Type:     schema.String,
				Desc:     "The MCP tool name to call, for example echo.",
				Required: true,
			},
			"arguments": {
				Type:     schema.Object,
				Desc:     "JSON arguments for the MCP tool.",
				Required: true,
			},
		}),
	}
}

// toolErrorJSON 将工具执行错误序列化为标准 JSON 错误格式 {"success": false, "error": "..."}。
func toolErrorJSON(ctx context.Context, err error) string {
	_ = ctx
	data, marshalErr := json.Marshal(map[string]any{
		"success": false,
		"error":   err.Error(),
	})
	if marshalErr != nil {
		return `{"success":false,"error":"tool execution failed"}`
	}
	return string(data)
}
