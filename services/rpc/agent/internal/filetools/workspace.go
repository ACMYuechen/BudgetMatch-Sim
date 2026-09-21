package filetools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// Workspace 是请求级目录能力；根目录句柄始终锚定当前认证用户，不接受模型传入身份。
// 调用方必须 Close。读写采用 os.Root，避免检查路径后再次按绝对路径打开的竞态。
type Workspace struct {
	root                        *os.Root
	maxReadBytes, maxWriteBytes int64
	extensions                  map[string]struct{}
	writePath                   string
	writeAttempted              atomic.Bool
}

// NewWorkspace 默认不启用。userID 必须来自认证上下文，writePath 只能来自本轮用户 /save 指令。
func NewWorkspace(cfg Config, userID, writePath string) (*Workspace, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if !secureFileIOSupported {
		return nil, errors.New("file tools require the supported Linux filesystem backend")
	}
	if strings.TrimSpace(userID) == "" || len(userID) > 256 || !utf8.ValidString(userID) {
		return nil, fmt.Errorf("file tools require authenticated identity: %w", os.ErrPermission)
	}
	cfg = cfg.Normalize()
	if cfg.MaxReadBytes > maxFileBytes || cfg.MaxWriteBytes > maxFileBytes {
		return nil, errors.New("file size configuration exceeds hard limit")
	}
	extensions := make(map[string]struct{}, len(cfg.WritableExtensions))
	for _, ext := range cfg.WritableExtensions {
		switch ext {
		case ".json", ".md", ".txt":
			extensions[ext] = struct{}{}
		default:
			return nil, errors.New("unsupported file extension configuration")
		}
	}
	if writePath != "" {
		if !cfg.AllowWrite {
			return nil, fmt.Errorf("file writes are disabled: %w", os.ErrPermission)
		}
		var err error
		writePath, err = cleanRelativePath(writePath)
		if err != nil {
			return nil, err
		}
		if _, ok := extensions[strings.ToLower(filepath.Ext(writePath))]; !ok {
			return nil, fmt.Errorf("file type is not permitted: %w", os.ErrPermission)
		}
	}
	if err := os.MkdirAll(cfg.Workspace, 0o700); err != nil {
		return nil, err
	}
	base, err := os.OpenRoot(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	users, err := openPrivateDirectory(base, "users")
	if err != nil {
		return nil, err
	}
	defer users.Close()
	// 哈希只用于稳定命名，不是认证手段；用户原始 ID 不能影响路径层级。
	namespace := fmt.Sprintf("%x", sha256.Sum256([]byte(userID)))
	root, err := openPrivateDirectory(users, namespace)
	if err != nil {
		return nil, err
	}
	return &Workspace{root: root, maxReadBytes: cfg.MaxReadBytes, maxWriteBytes: cfg.MaxWriteBytes, extensions: extensions, writePath: writePath}, nil
}

// openPrivateDirectory 拒绝预置的目录别名，并核对打开句柄与检查时的 inode 一致。
// 后续所有操作只使用打开的根句柄，目录被重命名也不会改用攻击者替换后的路径。
func openPrivateDirectory(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("workspace directory is not private: %w", os.ErrPermission)
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, fmt.Errorf("workspace directory changed during open: %w", os.ErrPermission)
	}
	return root, nil
}

func (w *Workspace) Close() error   { return w.root.Close() }
func (w *Workspace) CanWrite() bool { return w != nil && w.writePath != "" }

func (w *Workspace) checkedPath(name string) (string, error) {
	name, err := cleanRelativePath(name)
	if err != nil {
		return "", err
	}
	if _, ok := w.extensions[strings.ToLower(filepath.Ext(name))]; !ok {
		return "", fmt.Errorf("file type is not permitted: %w", os.ErrPermission)
	}
	return name, nil
}

func (w *Workspace) ReadFile(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	relative, err := w.checkedPath(name)
	if err != nil {
		return "", err
	}
	file, err := openRegularFile(w.root, relative)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > w.maxReadBytes {
		return "", errors.New("file exceeds maximum read size")
	}
	data, err := io.ReadAll(io.LimitReader(file, w.maxReadBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > w.maxReadBytes {
		return "", errors.New("file exceeds maximum read size")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("file content is not UTF-8 text")
	}
	return string(data), nil
}

// WriteFile 仅允许本轮授权路径的一次创建；不覆盖已有文件，不向读取方暴露半写入内容。
// 文件发布与会话事务不是同一事务；发布后取消/保存失败可能留下文件，重试不会覆盖它。
func (w *Workspace) WriteFile(ctx context.Context, name, content string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	relative, err := w.checkedPath(name)
	if err != nil {
		return 0, err
	}
	if w.writePath == "" || relative != w.writePath {
		return 0, fmt.Errorf("file write not authorized for this request: %w", os.ErrPermission)
	}
	if int64(len(content)) > w.maxWriteBytes || !utf8.ValidString(content) {
		return 0, errors.New("file write exceeds size limit or is not UTF-8")
	}
	if !w.writeAttempted.CompareAndSwap(false, true) {
		return 0, fmt.Errorf("file write already attempted: %w", os.ErrPermission)
	}
	parent := filepath.Dir(relative)
	if err := w.root.MkdirAll(parent, 0o700); err != nil {
		return 0, err
	}
	temporary := filepath.Join(parent, ".agent-"+rand.Text()+".tmp")
	file, err := w.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	defer w.root.Remove(temporary)
	_, writeErr := io.WriteString(file, content)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return 0, writeErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Link 原子发布且目标存在时失败；不同请求也不能覆盖同一路径。
	if err := w.root.Link(temporary, relative); err != nil {
		return 0, err
	}
	return len(content), nil
}

func cleanRelativePath(name string) (string, error) {
	deny := func() (string, error) { return "", fmt.Errorf("invalid relative file path: %w", os.ErrPermission) }
	if name == "" || name != strings.TrimSpace(name) || len(name) > 512 || !utf8.ValidString(name) {
		return deny()
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return deny()
		}
	}
	slashPath := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(slashPath, "/") || filepath.IsAbs(name) || strings.Contains(slashPath, ":") {
		return deny()
	}
	parts := strings.Split(slashPath, "/")
	if len(parts) > 8 {
		return deny()
	}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") {
			return deny()
		}
	}
	return filepath.FromSlash(slashPath), nil
}

// ParseSaveRequest 只识别当前用户原始消息第一行的 /save 指令，不从历史或工具输出推导授权。
// 第二行开始保留购物请求；路径控制信息不参与预算和关键词解析。
func ParseSaveRequest(query string) (shoppingQuery, writePath string, err error) {
	if !strings.HasPrefix(query, "/save ") {
		return query, "", nil
	}
	first, rest, ok := strings.Cut(query, "\n")
	if !ok || strings.TrimSpace(rest) == "" {
		return "", "", errors.New("save directive requires a shopping request on the next line")
	}
	name, err := cleanRelativePath(strings.TrimSuffix(strings.TrimPrefix(first, "/save "), "\r"))
	if err != nil {
		return "", "", err
	}
	return rest, name, nil
}
