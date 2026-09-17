// Package filetools 提供 LLM 文件工具使用的受限工作目录。
package filetools

import "strings"

const (
	defaultWorkspace    = "workspace/agent"
	defaultMaxReadBytes = int64(64 << 10)
	maxFileBytes        = int64(1 << 20)
)

var defaultWritableExtensions = []string{".json", ".md", ".txt"}

// Config 定义 read_file 和 write_file 的文件系统访问范围与限制。
type Config struct {
	Enabled            bool     `json:"enabled,optional"`            // 默认关闭，不创建目录或注册文件工具
	AllowWrite         bool     `json:"allowWrite,optional"`         // 写入另需本轮 /save 指令授权
	Workspace          string   `json:"workspace,optional"`          // Workspace 工作目录根路径，默认 "workspace/agent"
	MaxReadBytes       int64    `json:"maxReadBytes,optional"`       // 默认 64 KiB，硬上限 1 MiB
	MaxWriteBytes      int64    `json:"maxWriteBytes,optional"`      // 默认 64 KiB，硬上限 1 MiB
	WritableExtensions []string `json:"writableExtensions,optional"` // 兼容原配置名，同时限制读写后缀，只能是 .json/.md/.txt 的子集
}

// Normalize 返回填充安全默认值并统一文件后缀格式后的配置。
func (c Config) Normalize() Config {
	if strings.TrimSpace(c.Workspace) == "" {
		c.Workspace = defaultWorkspace
	}
	if c.MaxReadBytes <= 0 {
		c.MaxReadBytes = defaultMaxReadBytes
	}
	if c.MaxWriteBytes <= 0 {
		c.MaxWriteBytes = defaultMaxReadBytes
	}
	if len(c.WritableExtensions) == 0 {
		c.WritableExtensions = append([]string(nil), defaultWritableExtensions...)
	} else {
		c.WritableExtensions = append([]string(nil), c.WritableExtensions...)
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
