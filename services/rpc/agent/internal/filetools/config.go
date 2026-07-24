// Package filetools 提供 LLM 文件工具使用的受限工作目录。
package filetools

import "strings"

const (
	defaultWorkspace    = "workspace/agent"
	defaultMaxReadBytes = int64(1 << 20)
)

var defaultWritableExtensions = []string{".json", ".md", ".txt"}

// Config 定义 read_file 和 write_file 的文件系统访问范围与限制。
type Config struct {
	Workspace          string   `json:"workspace,optional"`          // Workspace 工作目录根路径，默认 "workspace/agent"
	MaxReadBytes       int64    `json:"maxReadBytes,optional"`       // MaxReadBytes 单次读取上限，默认 1MB
	WritableExtensions []string `json:"writableExtensions,optional"` // WritableExtensions 允许写入的文件后缀白名单，默认 .json/.md/.txt
}

// Normalize 返回填充安全默认值并统一文件后缀格式后的配置。
func (c Config) Normalize() Config {
	if strings.TrimSpace(c.Workspace) == "" {
		c.Workspace = defaultWorkspace
	}
	if c.MaxReadBytes <= 0 {
		c.MaxReadBytes = defaultMaxReadBytes
	}
	if len(c.WritableExtensions) == 0 {
		c.WritableExtensions = append([]string(nil), defaultWritableExtensions...)
	}
	for i, ext := range c.WritableExtensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext != "" && !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		c.WritableExtensions[i] = ext
	}
	return c
}
