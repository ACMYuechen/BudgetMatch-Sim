//go:build linux

package filetools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

func userWorkspace(t *testing.T, cfg Config, user, grant string) *Workspace {
	t.Helper()
	w, err := NewWorkspace(cfg, user, grant)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func TestWorkspaceDisabledAndIdentityChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unused")
	if w, err := NewWorkspace(Config{Workspace: path}, "", ""); w != nil || err != nil {
		t.Fatalf("disabled: %v %v", w, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disabled tool created a directory")
	}
	for _, id := range []string{"", "  ", strings.Repeat("x", 257), string([]byte{0xff})} {
		if _, err := NewWorkspace(Config{Enabled: true, Workspace: path}, id, ""); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("invalid identity accepted: %v", err)
		}
	}
}

func TestWorkspaceUserIsolationAndPrivatePermissions(t *testing.T) {
	cfg := Config{Enabled: true, AllowWrite: true, Workspace: t.TempDir()}
	a := userWorkspace(t, cfg, "../../alice", "saved.txt")
	b := userWorkspace(t, cfg, "bob", "saved.txt")
	if _, err := a.WriteFile(context.Background(), "saved.txt", "alice-private"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadFile(context.Background(), "saved.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cross-user read: %v", err)
	}
	if _, err := b.WriteFile(context.Background(), "saved.txt", "bob-private"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		w    *Workspace
		want string
	}{{a, "alice-private"}, {b, "bob-private"}} {
		got, err := tc.w.ReadFile(context.Background(), "saved.txt")
		if err != nil || got != tc.want {
			t.Fatalf("isolation: %q %v", got, err)
		}
		for _, name := range []string{".", "saved.txt"} {
			info, err := tc.w.root.Stat(name)
			if err != nil || info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("private mode %s: %v %v", name, info, err)
			}
		}
	}
}

func TestWorkspaceWriteRequiresExactCurrentGrant(t *testing.T) {
	cfg := Config{Enabled: true, Workspace: t.TempDir()}
	if _, err := NewWorkspace(cfg, "u", "report.md"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("operator write permission: %v", err)
	}
	cfg.AllowWrite = true
	w := userWorkspace(t, cfg, "u", "")
	if w.CanWrite() {
		t.Fatal("no grant registered a writer")
	}
	if _, err := w.WriteFile(context.Background(), "report.md", "body"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("missing grant: %v", err)
	}
	w = userWorkspace(t, cfg, "u", "report.md")
	if _, err := w.WriteFile(context.Background(), "other.md", "body"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("wrong path: %v", err)
	}
	if _, err := w.WriteFile(context.Background(), "report.md", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteFile(context.Background(), "report.md", "second"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("second attempt: %v", err)
	}
	retry := userWorkspace(t, cfg, "u", "report.md")
	if _, err := retry.WriteFile(context.Background(), "report.md", "retry"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrote existing: %v", err)
	}
	got, _ := retry.ReadFile(context.Background(), "report.md")
	if got != "first" {
		t.Fatal(got)
	}
}

func TestWorkspaceWriteLimitsAndCancellation(t *testing.T) {
	w := newTestWorkspace(t, Config{Workspace: t.TempDir(), MaxWriteBytes: 4}, "x.txt")
	for _, body := range []string{"12345", string([]byte{0xff})} {
		if _, err := w.WriteFile(context.Background(), "x.txt", body); err == nil {
			t.Fatal("invalid body accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.WriteFile(ctx, "x.txt", "1234"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := w.WriteFile(context.Background(), "x.txt", "1234"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadFile(ctx, "x.txt"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWorkspaceConcurrentWriteCreatesExactlyOneFile(t *testing.T) {
	w := newTestWorkspace(t, Config{Workspace: t.TempDir()}, "nested/report.md")
	var success atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := w.WriteFile(context.Background(), "nested/report.md", "body"); err == nil {
				success.Add(1)
			}
		})
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("successful writes: %d", success.Load())
	}
	entries, err := w.root.Open("nested")
	if err != nil {
		t.Fatal(err)
	}
	defer entries.Close()
	names, err := entries.Readdirnames(-1)
	if err != nil || len(names) != 1 || names[0] != "report.md" {
		t.Fatalf("partial files: %v %v", names, err)
	}
}

func TestWorkspaceRejectsAliasesAndNonRegularFiles(t *testing.T) {
	w := newTestWorkspace(t, Config{Workspace: t.TempDir()})
	if err := w.root.WriteFile("original.txt", []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.root.Link("original.txt", "hard.txt"); err != nil {
		t.Fatal(err)
	}
	if err := w.root.Symlink("original.txt", "symbolic.txt"); err != nil {
		t.Fatal(err)
	}
	if err := w.root.Mkdir("directory.txt", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(w.root.Name(), "pipe.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"original.txt", "hard.txt", "symbolic.txt", "directory.txt", "pipe.txt", ".env.txt", "keys.pem"} {
		if _, err := w.ReadFile(context.Background(), name); err == nil {
			t.Fatalf("unsafe file accepted: %s", name)
		}
	}
}

func TestWorkspaceRejectsPreplacedNamespaceAlias(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "users")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkspace(Config{Enabled: true, Workspace: root}, "u", ""); !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	root = t.TempDir()
	users := filepath.Join(root, "users")
	if err := os.Mkdir(users, 0o700); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%x", sha256.Sum256([]byte("u")))
	if err := os.Symlink(outside, filepath.Join(users, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkspace(Config{Enabled: true, Workspace: root}, "u", ""); !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
}

func TestWorkspaceSymlinkReplacementCannotEscape(t *testing.T) {
	w := newTestWorkspace(t, Config{Workspace: t.TempDir()}, "swap/new.txt")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.root.Mkdir("inside", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := w.root.WriteFile("inside/secret.txt", []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 250 {
			_ = w.root.Remove("swap")
			_ = w.root.Symlink(outside, "swap")
			_ = w.root.Remove("swap")
			_ = w.root.Symlink("inside", "swap")
		}
	})
	for range 250 {
		content, err := w.ReadFile(context.Background(), "swap/secret.txt")
		if err == nil && content != "inside" {
			t.Errorf("escaped: %q", content)
		}
		_, _ = w.WriteFile(context.Background(), "swap/new.txt", "must stay inside")
	}
	wg.Wait()
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("write escaped: %v", err)
	}
}

func TestParseSaveRequest(t *testing.T) {
	for _, tc := range []struct {
		query, path, text string
		bad               bool
	}{
		{"/save reports/3000元.md\n预算500元买键盘", "reports/3000元.md", "预算500元买键盘", false},
		{"/save a.md\r\n预算500元", "a.md", "预算500元", false},
		{"请保存到 a.md", "", "请保存到 a.md", false},
		{"正文\n/save a.md", "", "正文\n/save a.md", false},
		{"/save ../a.md\n买键盘", "", "", true},
		{"/save a.md", "", "", true},
		{"/save a.md\n  ", "", "", true},
	} {
		text, path, err := ParseSaveRequest(tc.query)
		if (err != nil) != tc.bad || (!tc.bad && (text != tc.text || path != tc.path)) {
			t.Errorf("parse %q: %q %q %v", tc.query, text, path, err)
		}
	}
}

func FuzzRelativePath(f *testing.F) {
	for _, seed := range []string{"a.md", "../secret.txt", `C:\x.txt`, "x//a.md", ".env", "a\x00.txt", "a/b.txt"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		got, err := cleanRelativePath(name)
		if err != nil {
			return
		}
		if !filepath.IsLocal(got) || len(got) > 512 || strings.Contains(got, ":") {
			t.Fatalf("non-local accepted: %q", got)
		}
		for _, part := range strings.Split(filepath.ToSlash(got), "/") {
			if part == "" || strings.HasPrefix(part, ".") {
				t.Fatalf("hidden/traversal: %q", got)
			}
		}
	})
}
