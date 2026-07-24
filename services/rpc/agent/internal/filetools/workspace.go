package filetools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Workspace 提供限制在指定工作目录内的文件读写能力。
type Workspace struct {
	root               string
	maxReadBytes       int64
	writableExtensions map[string]struct{}
}

// NewWorkspace 根据配置创建受限文件工作区。
func NewWorkspace(cfg Config) (*Workspace, error) {
	cfg = cfg.Normalize()
	// 将相对工作目录转为绝对路径，确保后续路径比较可靠。
	root, err := filepath.Abs(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve file tools workspace: %w", err)
	}

	// 将后缀白名单转为 map，WriteFile 时 O(1) 查找。
	extensions := make(map[string]struct{}, len(cfg.WritableExtensions))
	for _, ext := range cfg.WritableExtensions {
		if ext != "" {
			extensions[ext] = struct{}{}
		}
	}
	if len(extensions) == 0 {
		return nil, errors.New("file tools writable extensions cannot be empty")
	}

	return &Workspace{
		root:               filepath.Clean(root), // 规范化掉尾部斜杠和冗余分隔符
		maxReadBytes:       cfg.MaxReadBytes,
		writableExtensions: extensions,
	}, nil
}

// name 必须是相对于工作目录的相对路径，不能包含 .. 或绝对路径。
// 超过 MaxReadBytes 的文件会被拒绝，防止 LLM 读取过大的文件超出上下文窗口。
func (w *Workspace) ReadFile(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// 校验路径合法性：禁止空路径、绝对路径、.. 穿越。
	relative, err := cleanRelativePath(name)
	if err != nil {
		return "", err
	}

	// 解析工作目录的符号链接，得到真实的根路径。
	root, err := filepath.EvalSymlinks(w.root)
	if err != nil {
		return "", fmt.Errorf("resolve file tools workspace: %w", err)
	}
	// 拼接后再次解析符号链接，防止通过符号链接逃逸。
	target, err := filepath.EvalSymlinks(filepath.Join(root, relative))
	if err != nil {
		return "", fmt.Errorf("resolve file %q: %w", name, err)
	}
	if !isWithin(root, target) {
		return "", fmt.Errorf("file path %q escapes the agent workspace", name)
	}

	file, err := os.Open(target)
	if err != nil {
		return "", fmt.Errorf("open file %q: %w", name, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file %q: %w", name, err)
	}
	// 拒绝目录、设备文件等非普通文件。
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file path %q is not a regular file", name)
	}
	// 第一道大小检查：用 stat 快速拒绝明显超限的文件。
	if info.Size() > w.maxReadBytes {
		return "", fmt.Errorf("file %q exceeds maximum read size of %d bytes", name, w.maxReadBytes)
	}

	// 第二道大小检查：以 maxReadBytes+1 读取，若实际读到超限字节则拒绝，
	// 防止 stat 大小与实际内容不一致（如 /proc 下的伪文件）。
	data, err := io.ReadAll(io.LimitReader(file, w.maxReadBytes+1))
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", name, err)
	}
	if int64(len(data)) > w.maxReadBytes {
		return "", fmt.Errorf("file %q exceeds maximum read size of %d bytes", name, w.maxReadBytes)
	}
	return string(data), nil
}

// WriteFile 在工作目录内写入文件，返回实际写入的字节数。
// name 必须是相对于工作目录的相对路径，后缀必须在 WritableExtensions 白名单中。
// 写入前会检查符号链接是否逃逸工作目录，防止 LLM 通过符号链接篡改系统文件。
func (w *Workspace) WriteFile(ctx context.Context, name, content string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	relative, err := cleanRelativePath(name)
	if err != nil {
		return 0, err
	}
	// 后缀白名单检查，仅允许写入指定类型的文件。
	extension := strings.ToLower(filepath.Ext(relative))
	if _, ok := w.writableExtensions[extension]; !ok {
		return 0, fmt.Errorf("file extension %q is not writable", extension)
	}

	// 确保工作目录根存在，后续解析符号链接和创建父目录都依赖它。
	if err := os.MkdirAll(w.root, 0o755); err != nil {
		return 0, fmt.Errorf("create file tools workspace: %w", err)
	}
	root, err := filepath.EvalSymlinks(w.root)
	if err != nil {
		return 0, fmt.Errorf("resolve file tools workspace: %w", err)
	}
	// 逐级创建父目录，同时检查目录符号链接不逃逸。
	parent, err := ensureDirectory(root, filepath.Dir(relative))
	if err != nil {
		return 0, fmt.Errorf("resolve parent directory for %q: %w", name, err)
	}

	target := filepath.Join(parent, filepath.Base(relative))
	// 如果目标文件已存在，检查是否为符号链接并解析，确保写入仍在工作目录内。
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, err = filepath.EvalSymlinks(target)
			if err != nil {
				return 0, fmt.Errorf("resolve file %q: %w", name, err)
			}
		}
		if !isWithin(root, target) {
			return 0, fmt.Errorf("file path %q escapes the agent workspace", name)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return 0, fmt.Errorf("inspect file %q: %w", name, statErr)
	}

	data := []byte(content)
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return 0, fmt.Errorf("write file %q: %w", name, err)
	}
	return len(data), nil
}

// cleanRelativePath 校验并标准化相对路径，拒绝空路径、绝对路径、含 .. 的路径和空字节。
func cleanRelativePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	// 拒绝空路径和含空字节的路径（空字节常用于路径截断攻击）。
	if name == "" || strings.ContainsRune(name, '\x00') {
		return "", errors.New("file path must be a non-empty relative path")
	}

	// 统一分隔符为 /，再检测是否为绝对路径（Unix /、Windows 盘符、平台原生）。
	slashPath := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(slashPath, "/") || filepath.IsAbs(name) || hasWindowsVolume(slashPath) {
		return "", fmt.Errorf("absolute file path %q is not allowed", name)
	}
	// 逐段检查，拒绝 .. 目录穿越。
	for _, part := range strings.Split(slashPath, "/") {
		if part == ".." {
			return "", fmt.Errorf("file path %q cannot contain ..", name)
		}
	}

	// 转为平台原生分隔符并清理冗余 . 和多余分隔符。
	cleaned := filepath.Clean(filepath.FromSlash(slashPath))
	if cleaned == "." {
		return "", errors.New("file path must identify a file")
	}
	return cleaned, nil
}

// hasWindowsVolume 判断路径是否以 Windows 盘符开头（如 C:），用于跨平台拒绝绝对路径。
func hasWindowsVolume(name string) bool {
	return len(name) >= 2 && name[1] == ':' &&
		((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z'))
}

// ensureDirectory 在工作目录 root 下逐级创建 relative 指定的目录链，返回最终目录的绝对路径。
// 过程中会跟踪符号链接，防止通过目录符号链接逃逸出工作目录。
func ensureDirectory(root, relative string) (string, error) {
	current := root
	if relative == "." {
		return current, nil
	}
	// 逐级遍历路径分量，处理四种情况。
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// 目录不存在，创建之。
			if err := os.Mkdir(next, 0o755); err != nil {
				return "", err
			}
		case err != nil:
			return "", err
		case info.Mode()&os.ModeSymlink != 0:
			// 是符号链接：解析后检查不逃逸，并确认指向目录。
			next, err = filepath.EvalSymlinks(next)
			if err != nil {
				return "", err
			}
			if !isWithin(root, next) {
				return "", errors.New("directory symlink escapes the agent workspace")
			}
			info, err = os.Stat(next)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				return "", errors.New("path component is not a directory")
			}
		case !info.IsDir():
			// 已存在但不是目录（如普通文件），拒绝。
			return "", errors.New("path component is not a directory")
		}
		current = next
	}
	return current, nil
}

// isWithin 判断 target 路径是否在 root 目录内（不是 root 自身且不以 .. 开头）。
func isWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	// Rel 对 root 自身返回 "."，对上级返回 ".." 或 "../..."，必须同时排除。
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
