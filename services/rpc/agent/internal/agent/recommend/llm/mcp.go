package llm

import (
	"context"
	"fmt"

	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// mcpTools 启动 MCP server 并把它暴露的工具转成 Eino 工具列表。
//
// 与旧实现最大的区别：每个 MCP 工具都通过 eino-ext 的 mcp.GetTools 成为「一等」Eino 工具，
// 携带自己的名称和参数 schema 直接进入 ReAct 的工具集，模型能看清每个工具怎么调用；
// 不再藏在一个 mcp_call_tool 元工具后面让模型盲传 name/arguments。
//
// 返回的 cleanup 负责在请求结束后关闭 MCP 子进程；MCP 未启用时返回空工具集和空 cleanup。
func mcpTools(ctx context.Context, cfg mcpconfig.Config, s *session) ([]tool.BaseTool, func(), error) {
	noop := func() {}
	if !cfg.Ready() {
		return nil, noop, nil
	}

	cli, err := mcpclient.NewStdioMCPClient(cfg.Command, nil, cfg.Args...)
	if err != nil {
		return nil, noop, fmt.Errorf("start mcp client: %w", err)
	}
	cleanup := func() { _ = cli.Close() }

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "budgetmatch-sim-agent", Version: "0.1.0"}

	initCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout())
	defer cancel()
	if _, err := cli.Initialize(initCtx, initReq); err != nil {
		cleanup()
		return nil, noop, fmt.Errorf("initialize mcp client: %w", err)
	}

	baseTools, err := einomcp.GetTools(ctx, &einomcp.Config{Cli: cli})
	if err != nil {
		cleanup()
		return nil, noop, fmt.Errorf("load mcp tools: %w", err)
	}

	out := make([]tool.BaseTool, 0, len(baseTools))
	for _, bt := range baseTools {
		out = append(out, decorate(s, "", bt))
	}
	return out, cleanup, nil
}
