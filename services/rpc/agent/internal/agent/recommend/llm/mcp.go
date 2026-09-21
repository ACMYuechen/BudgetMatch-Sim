package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mcpTools 仅加载显式允许且声明只读的工具。声明不是沙箱；操作员仍需审计服务端。
func mcpTools(ctx context.Context, cfg mcpconfig.Config, s *session) ([]tool.BaseTool, func(), error) {
	noop := func() {}
	if !cfg.Ready() {
		return nil, noop, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	if cfg.Validate() != nil {
		return nil, noop, status.Error(codes.PermissionDenied, "invalid MCP policy configuration")
	}
	cli, cleanup, err := startMCP(ctx, cfg)
	if err != nil {
		return nil, noop, safety.Protect(err)
	}
	initCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout())
	defer cancel()
	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "budgetmatch-sim-agent", Version: "0.1.0"}
	if _, err := cli.Initialize(initCtx, initReq); err != nil {
		cleanup()
		return nil, noop, safety.Protect(err)
	}
	tools, err := loadMCPTools(ctx, cfg, cli, s)
	if err != nil {
		cleanup()
		return nil, noop, safety.Protect(err)
	}
	return tools, cleanup, nil
}

func startMCP(ctx context.Context, cfg mcpconfig.Config) (*mcpclient.Client, func(), error) {
	processCtx, cancel := context.WithCancel(ctx)
	directory, err := os.MkdirTemp("", "agent-mcp-")
	if err != nil {
		cancel()
		return nil, nil, err
	}
	stdio := transport.NewStdioWithOptions(cfg.Command, nil, cfg.Args,
		transport.WithCommandFunc(func(_ context.Context, command string, _ []string, args []string) (*exec.Cmd, error) {
			if err := processCtx.Err(); err != nil {
				return nil, err
			}
			cmd := exec.CommandContext(processCtx, command, args...)
			// 不继承 JWT、数据库、模型密钥或代理变量；不在仓库/用户文件目录启动。
			cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8"}
			cmd.Dir = directory
			cmd.WaitDelay = time.Second
			if err := confineMCPProcess(cmd); err != nil {
				return nil, err
			}
			return cmd, nil
		}), transport.WithCommandLogger(mcpMetadataLogger{}))
	if err := stdio.Start(processCtx); err != nil {
		cancel()
		os.RemoveAll(directory)
		return nil, nil, err
	}
	cli := mcpclient.NewClient(stdio)
	drained := make(chan struct{})
	go func() { defer close(drained); _, _ = io.Copy(io.Discard, stdio.Stderr()) }()
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancel() // 先停止本请求进程组，再等待旧版 MCP 客户端 Close，避免等待不退出的子进程。
			_ = cli.Close()
			<-drained
			_ = os.RemoveAll(directory)
		})
	}
	return cli, cleanup, nil
}

// SDK 的 transport 日志也不能绕过错误脱敏；stderr 仅排空，不持久化正文。
type mcpMetadataLogger struct{}

func (mcpMetadataLogger) Infof(string, ...any)  { logx.Info("MCP transport event") }
func (mcpMetadataLogger) Errorf(string, ...any) { logx.Error("MCP transport error") }

func loadMCPTools(ctx context.Context, cfg mcpconfig.Config, cli mcpclient.MCPClient, s *session) ([]tool.BaseTool, error) {
	if !cfg.Ready() {
		return nil, nil
	}
	if cfg.Validate() != nil {
		return nil, status.Error(codes.PermissionDenied, "invalid MCP policy configuration")
	}
	policy := &policyMCPClient{MCPClient: cli, timeout: cfg.RequestTimeout(), allowed: make(map[string]bool), validated: make(map[string]bool)}
	for _, name := range cfg.AllowedTools {
		policy.allowed[name] = true
	}
	baseTools, err := einomcp.GetTools(ctx, &einomcp.Config{Cli: policy, ToolNameList: cfg.AllowedTools})
	if err != nil {
		return nil, err
	}
	out := make([]tool.BaseTool, 0, len(baseTools))
	for _, base := range baseTools {
		info, err := base.Info(ctx)
		if err != nil || info == nil {
			return nil, errors.New("invalid MCP tool metadata")
		}
		inv, ok := base.(tool.InvokableTool)
		if !ok {
			return nil, errors.New("unsupported MCP tool interface")
		}
		// 模型可见的工具名使用独立命名空间，避免冒充内部商品工具。
		aliased := &namedMCPTool{InvokableTool: inv, name: "mcp_" + info.Name}
		out = append(out, decorate(s, aliased.name, aliased))
	}
	return out, nil
}

type namedMCPTool struct {
	tool.InvokableTool
	name string
}

func (t *namedMCPTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.InvokableTool.Info(ctx)
	if err != nil {
		return nil, err
	}
	copy := *info
	copy.Name = t.name
	return &copy, nil
}

// policyMCPClient 在工具发现和实际调用两处执行白名单；调用方不能通过修改元信息绕过。
type policyMCPClient struct {
	mcpclient.MCPClient
	timeout   time.Duration
	allowed   map[string]bool
	mu        sync.RWMutex
	validated map[string]bool
}

func (p *policyMCPClient) ListTools(ctx context.Context, _ mcpgo.ListToolsRequest) (*mcpgo.ListToolsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &mcpgo.ListToolsResult{}
	validated := make(map[string]bool)
	seenCursors := make(map[mcpgo.Cursor]bool)
	request := mcpgo.ListToolsRequest{}
	total := 0
	for page := 0; page < 8; page++ {
		listed, err := p.MCPClient.ListToolsByPage(ctx, request)
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		if err != nil {
			return nil, err
		}
		if listed == nil {
			return nil, errors.New("empty MCP tools response")
		}
		total += len(listed.Tools)
		if total > 128 {
			return nil, safety.ErrOutputLimit
		}
		for _, candidate := range listed.Tools {
			if !p.allowed[candidate.Name] {
				continue
			}
			if validated[candidate.Name] || candidate.Annotations.ReadOnlyHint == nil || !*candidate.Annotations.ReadOnlyHint {
				return nil, status.Error(codes.PermissionDenied, "MCP tool is duplicate or not declared read-only")
			}
			validated[candidate.Name] = true
			result.Tools = append(result.Tools, candidate)
		}
		if listed.NextCursor == "" {
			if len(validated) != len(p.allowed) {
				return nil, status.Error(codes.PermissionDenied, "allowed MCP tool unavailable")
			}
			p.mu.Lock()
			p.validated = validated
			p.mu.Unlock()
			return result, nil
		}
		if seenCursors[listed.NextCursor] {
			return nil, errors.New("repeated MCP tools cursor")
		}
		seenCursors[listed.NextCursor] = true
		request.Params.Cursor = listed.NextCursor
	}
	return nil, safety.ErrOutputLimit
}

func (p *policyMCPClient) CallTool(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	permitted := p.validated[request.Params.Name]
	p.mu.RUnlock()
	if !permitted {
		return nil, status.Error(codes.PermissionDenied, "MCP tool not allowed")
	}
	args, err := json.Marshal(request.Params.Arguments)
	if err != nil || len(args) > maxToolPayloadBytes {
		return nil, status.Error(codes.InvalidArgument, "invalid MCP arguments")
	}
	request.Header, request.Params.Meta = nil, nil
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	result, err := p.MCPClient.CallTool(ctx, request)
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err != nil {
		return nil, safety.Protect(err)
	}
	if result == nil || result.IsError {
		return nil, errors.New("MCP tool execution failed")
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > maxToolPayloadBytes {
		return nil, safety.ErrOutputLimit
	}
	return result, nil
}
