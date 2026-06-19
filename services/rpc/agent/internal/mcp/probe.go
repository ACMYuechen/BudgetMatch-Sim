package mcp

import (
	"context"
	"fmt"
	"strings"
)

// ProbeResult MCP 探测结果，包含工具数量、工具名称和探测消息。
type ProbeResult struct {
	ToolCount int    // 可用工具总数
	ToolName  string // 探测时使用的工具名称（优先使用 "echo"）
	Message   string // 探测工具调用的返回消息
}

// Probe 对指定的 MCP Server 进行探测：连接 -> 列出工具 -> 尝试调用 echo 工具。
// 若 echo 工具不存在，则返回连接成功但无 echo 工具的提示信息。
func Probe(ctx context.Context, cfg Config) (*ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout())
	defer cancel()

	client := NewClient(cfg)
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	defer client.Close()

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	toolName := firstToolName(tools, "echo")
	if toolName == "" {
		return &ProbeResult{
			ToolCount: len(tools),
			Message:   "MCP server connected, but echo tool was not found.",
		}, nil
	}

	result, err := client.CallTool(ctx, toolName, map[string]any{
		"message": "budgetmatch agent mcp probe",
	})
	if err != nil {
		return nil, err
	}

	return &ProbeResult{
		ToolCount: len(tools),
		ToolName:  toolName,
		Message:   firstText(result),
	}, nil
}

// firstToolName 在工具列表中查找指定名称的工具，返回匹配的工具名；未找到则返回空字符串。
func firstToolName(tools []Tool, name string) string {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Name
		}
	}
	return ""
}

// firstText 从工具调用结果中提取所有文本内容并拼接；若无文本内容则返回内容块数量描述。
func firstText(result *ToolResult) string {
	if result == nil {
		return ""
	}
	var texts []string
	for _, content := range result.Content {
		if content.Text != "" {
			texts = append(texts, content.Text)
		}
	}
	if len(texts) == 0 {
		return fmt.Sprintf("%d content blocks", len(result.Content))
	}
	return strings.Join(texts, "\n")
}
