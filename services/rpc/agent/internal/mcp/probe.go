package mcp

import (
	"context"
	"fmt"
	"strings"
)

type ProbeResult struct {
	ToolCount int
	ToolName  string
	Message   string
}

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

func firstToolName(tools []Tool, name string) string {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Name
		}
	}
	return ""
}

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
