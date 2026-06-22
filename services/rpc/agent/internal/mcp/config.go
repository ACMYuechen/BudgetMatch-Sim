package mcp

import "time"

// Config MCP 客户端配置，用于指定子进程启动命令、参数及超时时间。
type Config struct {
	Enabled bool     `json:"enabled,optional"` // 是否启用 MCP 客户端
	Command string   `json:"command,optional"` // MCP Server 启动命令（如 npx、python3）
	Args    []string `json:"args,optional"`    // 启动命令的参数列表
	Timeout int64    `json:"timeout,optional"` // 请求超时时间（毫秒，默认 5 秒）
}

// RequestTimeout 将配置中的毫秒超时转换为 time.Duration，若未配置则返回默认 5 秒。
func (c Config) RequestTimeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.Timeout) * time.Millisecond
}

// Ready 判断 MCP 客户端配置是否已就绪（Enabled 为 true 且 Command 不为空）。
func (c Config) Ready() bool {
	return c.Enabled && c.Command != ""
}
