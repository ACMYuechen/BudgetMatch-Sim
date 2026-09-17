package mcp

import (
	"errors"
	"path/filepath"
	"regexp"
	"time"
)

// Config MCP 客户端配置，用于指定子进程启动命令、参数及超时时间。
type Config struct {
	Enabled      bool     `json:"enabled,optional"`      // 是否启用 MCP 客户端
	Command      string   `json:"command,optional"`      // 已安装、经审计的 MCP Server 可执行文件绝对路径
	Args         []string `json:"args,optional"`         // 启动命令的参数列表
	Timeout      int64    `json:"timeout,optional"`      // 请求超时时间（毫秒，默认 5 秒）
	AllowedTools []string `json:"allowedTools,optional"` // 精确名称白名单；空列表拒绝全部工具且不启动进程
}

// RequestTimeout 将配置中的毫秒超时转换为 time.Duration，若未配置则返回默认 5 秒。
func (c Config) RequestTimeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Second
	}
	if c.Timeout > 30000 {
		return 30 * time.Second
	}
	return time.Duration(c.Timeout) * time.Millisecond
}

// Ready 判断是否显式启用并授权工具；命令与名称的合法性由 Validate 检查。
func (c Config) Ready() bool {
	return c.Enabled && len(c.AllowedTools) > 0
}

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,47}$`)

func (c Config) Validate() error {
	if !c.Ready() {
		return nil
	}
	if !filepath.IsAbs(c.Command) || len(c.AllowedTools) > 16 {
		return errors.New("MCP requires an absolute executable path and at most 16 allowed tools")
	}
	seen := map[string]bool{}
	for _, name := range c.AllowedTools {
		if !toolNamePattern.MatchString(name) || seen[name] {
			return errors.New("MCP tool allowlist contains invalid or duplicate names")
		}
		switch name {
		case "search_products", "select_bundle", "read_file", "write_file":
			return errors.New("MCP tool name conflicts with a built-in tool")
		}
		seen[name] = true
	}
	return nil
}
