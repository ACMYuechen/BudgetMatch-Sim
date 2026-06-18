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

type Client struct {
	command string
	args    []string
	timeout int64

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	stderr *tailBuffer

	nextID int64
	mu     sync.Mutex
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

func NewClient(c Config) *Client {
	return &Client{
		command: c.Command,
		args:    append([]string(nil), c.Args...),
		timeout: int64(c.RequestTimeout()),
	}
}

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

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

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

func (c *Client) notify(method string) error {
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	})
}

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

func (c *Client) stderrText() string {
	if c.stderr == nil {
		return ""
	}
	return c.stderr.String()
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   bytes.Buffer
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

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

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
