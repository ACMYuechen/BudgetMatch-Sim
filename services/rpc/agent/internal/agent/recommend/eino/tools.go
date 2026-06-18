package eino

import (
	"context"
	"encoding/json"

	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

type executorTool struct {
	name     string
	info     *schema.ToolInfo
	executor *toolkit.Executor
}

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

func (t *executorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return t.info, nil
}

func (t *executorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	result, err := t.executor.Execute(ctx, t.name, json.RawMessage(argumentsInJSON))
	if err != nil {
		return "", err
	}
	return string(result.JSON), nil
}

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
