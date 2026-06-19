package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

const protocolVersion = "2025-03-26"

// Client MCP（Model Context Protocol）客户端，通过 stdio 与子进程形式的 MCP Server 通信。
// 使用 JSON-RPC 2.0 协议进行请求/响应交互，支持工具列表查询和工具调用。
type Client struct {
	command string   // MCP Server 启动命令
	args    []string // MCP Server 启动参数
	timeout int64    // 请求超时时间（毫秒）

	cmd    *exec.Cmd      // 正在运行的子进程
	stdin  io.WriteCloser // 子进程标准输入
	reader *bufio.Reader  // 子进程标准输出读取器
	stderr *tailBuffer    // 子进程标准错误尾缓冲区，用于故障排查

	nextID int64      // 下一个 JSON-RPC 请求 ID（原子递增）
	mu     sync.Mutex // 保护写操作的互斥锁，确保请求/响应顺序一致
}

// Tool MCP Server 提供的工具定义。
type Tool struct {
	Name        string         `json:"name"`        // 工具名称
	Description string         `json:"description"` // 工具描述
	InputSchema map[string]any `json:"inputSchema"` // 工具输入参数 JSON Schema
}

// ToolContent 工具调用返回的内容块。
type ToolContent struct {
	Type string `json:"type"`           // 内容类型，如 "text"
	Text string `json:"text,omitempty"` // 文本内容
}

// ToolResult 工具调用的返回结果。
type ToolResult struct {
	Content []ToolContent `json:"content"`           // 内容块列表
	IsError bool          `json:"isError,omitempty"` // 是否表示错误
}

// rpcRequest JSON-RPC 2.0 请求结构。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`          // JSON-RPC 版本，固定为 "2.0"
	ID      int64  `json:"id,omitempty"`     // 请求标识，用于匹配响应
	Method  string `json:"method"`           // 调用的方法名
	Params  any    `json:"params,omitempty"` // 方法参数
}

// rpcResponse JSON-RPC 2.0 响应结构。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`          // JSON-RPC 版本
	ID      int64           `json:"id,omitempty"`     // 对应的请求标识
	Result  json.RawMessage `json:"result,omitempty"` // 成功时的结果数据
	Error   *rpcError       `json:"error,omitempty"`  // 错误时的错误信息
}

// rpcError JSON-RPC 2.0 错误结构。
type rpcError struct {
	Code    int64  `json:"code"`    // 错误码
	Message string `json:"message"` // 错误描述
}

// NewClient 根据配置创建 MCP 客户端实例，但不启动子进程。
func NewClient(c Config) *Client {
	return &Client{
		command: c.Command,
		args:    append([]string(nil), c.Args...),
		timeout: int64(c.RequestTimeout()),
	}
}

// Start 启动 MCP Server 子进程并完成初始化握手。
// 流程：启动子进程 -> 建立 stdin/stdout/stderr 管道 -> 执行 initialize 请求 -> 发送 initialized 通知。
func (c *Client) Start(ctx context.Context) error {
	if c.command == "" {
		return errors.New("mcp command is empty")
	}
	if c.cmd != nil {
		return nil
	}

	cmd := exec.CommandContext(ctx, c.command, c.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open mcp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open mcp stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open mcp stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mcp server: %w", err)
	}
	stderrBuffer := newTailBuffer(4096)
	go io.Copy(stderrBuffer, stderr)

	c.cmd = cmd
	c.stdin = stdin
	c.reader = bufio.NewReader(stdout)
	c.stderr = stderrBuffer

	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

// Close 关闭 MCP 客户端，终止子进程并清理资源。
func (c *Client) Close() error {
	var err error
	if c.stdin != nil {
		err = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	c.cmd = nil
	c.stdin = nil
	c.reader = nil
	c.stderr = nil
	return err
}

// ListTools 向 MCP Server 请求可用工具列表。
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool 调用 MCP Server 上的指定工具，传入参数并返回结果。
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	var result ToolResult
	if err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// initialize 执行 MCP 初始化握手：发送 initialize 请求，然后发送 initialized 通知。
func (c *Client) initialize(ctx context.Context) error {
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "budgetmatch-sim-agent",
			"version": "0.1.0",
		},
	}, &result); err != nil {
		return err
	}
	return c.notify("notifications/initialized")
}

// call 发送 JSON-RPC 请求并等待匹配的响应，支持上下文超时取消。
// 通过 ID 匹配请求与响应，忽略非目标响应（如 Server 主动推送的通知）。
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := atomic.AddInt64(&c.nextID, 1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.write(req); err != nil {
		return err
	}

	for {
		resp, err := c.read(ctx)
		if err != nil {
			return err
		}
		if resp.ID == 0 || resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("mcp %s failed: %s", method, resp.Error.Message)
		}
		if out == nil {
			return nil
		}
		if len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode mcp %s result: %w", method, err)
		}
		return nil
	}
}

// notify 发送 JSON-RPC 通知（无 ID，不需要响应）。
func (c *Client) notify(method string) error {
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	})
}

// write 将数据编码为 JSON 并写入子进程 stdin，末尾追加换行符。
func (c *Client) write(v any) error {
	if c.stdin == nil {
		return errors.New("mcp client is not started")
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode mcp request: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := c.stdin.Write(payload); err != nil {
		return fmt.Errorf("write mcp request: %w", err)
	}
	return nil
}

// read 从子进程 stdout 读取一行响应，支持上下文超时取消。
// 超时或读取失败时，会附加 stderr 中的最近错误信息以便排查问题。
func (c *Client) read(ctx context.Context) (*rpcResponse, error) {
	if c.reader == nil {
		return nil, errors.New("mcp client is not started")
	}

	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := c.reader.ReadString('\n')
		ch <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		if stderr := c.stderrText(); stderr != "" {
			return nil, fmt.Errorf("%w: %s", ctx.Err(), stderr)
		}
		return nil, ctx.Err()
	case result := <-ch:
		if result.err != nil {
			if stderr := c.stderrText(); stderr != "" {
				return nil, fmt.Errorf("read mcp response: %w: %s", result.err, stderr)
			}
			return nil, fmt.Errorf("read mcp response: %w", result.err)
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(result.line), &resp); err != nil {
			return nil, fmt.Errorf("decode mcp response: %w", err)
		}
		return &resp, nil
	}
}

// stderrText 返回子进程 stderr 中的最近输出内容，用于故障排查。
func (c *Client) stderrText() string {
	if c.stderr == nil {
		return ""
	}
	return c.stderr.String()
}

// tailBuffer 带大小限制的尾部缓冲区，用于保存子进程 stderr 的最后若干字节。
type tailBuffer struct {
	mu    sync.Mutex // 保护 buf 的互斥锁
	limit int        // 最大保留字节数
	buf   bytes.Buffer
}

// newTailBuffer 创建指定容量限制的尾部缓冲区。
func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

// Write 实现 io.Writer 接口，写入数据并在超出限制时保留尾部内容。
func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n := len(p)
	_, _ = b.buf.Write(p)
	if b.buf.Len() > b.limit {
		data := b.buf.Bytes()
		keep := append([]byte(nil), data[len(data)-b.limit:]...)
		b.buf.Reset()
		_, _ = b.buf.Write(keep)
	}
	return n, nil
}

// String 返回缓冲区中保存的字符串内容。
func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
