package filetools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceReadWrite(t *testing.T) {
	root := t.TempDir()
	workspace := newTestWorkspace(t, Config{Workspace: root})

	written, err := workspace.WriteFile(context.Background(), "recommendations/demo.md", "hello")
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if written != len("hello") {
		t.Fatalf("WriteFile() bytes = %d, want %d", written, len("hello"))
	}

	content, err := workspace.ReadFile(context.Background(), "recommendations/demo.md")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if content != "hello" {
		t.Fatalf("ReadFile() content = %q, want hello", content)
	}
}

func TestWorkspaceRejectsUnsafePaths(t *testing.T) {
	workspace := newTestWorkspace(t, Config{Workspace: t.TempDir()})
	absolute := filepath.Join(t.TempDir(), "outside.txt")

	for _, name := range []string{"../../.env", "../outside.txt", "nested/../outside.txt", absolute, `C:\outside.txt`} {
		t.Run(name, func(t *testing.T) {
			if _, err := workspace.ReadFile(context.Background(), name); err == nil {
				t.Fatalf("ReadFile(%q) expected error", name)
			}
			if _, err := workspace.WriteFile(context.Background(), name, "blocked"); err == nil {
				t.Fatalf("WriteFile(%q) expected error", name)
			}
		})
	}
}

func TestWorkspaceLimitsReadSize(t *testing.T) {
	root := t.TempDir()
	workspace := newTestWorkspace(t, Config{Workspace: root, MaxReadBytes: 4})
	if err := workspace.root.WriteFile("large.txt", []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := workspace.ReadFile(context.Background(), "large.txt"); err == nil ||
		!strings.Contains(err.Error(), "maximum read size") {
		t.Fatalf("ReadFile() error = %v, want size limit error", err)
	}
}

func TestWorkspaceRestrictsWritableExtensions(t *testing.T) {
	workspace := newTestWorkspace(t, Config{Workspace: t.TempDir()}, "data.JSON")

	if _, err := workspace.WriteFile(context.Background(), "script.go", "package main"); err == nil {
		t.Fatal("WriteFile() expected extension error")
	}
	if _, err := workspace.WriteFile(context.Background(), "data.JSON", "{}"); err != nil {
		t.Fatalf("WriteFile() allowed extension error = %v", err)
	}
}

func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := newTestWorkspace(t, Config{Workspace: root}, "escape/new.txt")
	if err := workspace.root.Symlink(outside, "escape"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := workspace.ReadFile(context.Background(), "escape/secret.txt"); err == nil {
		t.Fatal("ReadFile() expected symlink escape error")
	}
	if _, err := workspace.WriteFile(context.Background(), "escape/new.txt", "blocked"); err == nil {
		t.Fatal("WriteFile() expected symlink escape error")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was created, stat error = %v", err)
	}
}

func newTestWorkspace(t *testing.T, cfg Config, grants ...string) *Workspace {
	t.Helper()
	cfg.Enabled, cfg.AllowWrite = true, true
	grant := "recommendations/demo.md"
	if len(grants) > 0 {
		grant = grants[0]
	}
	workspace, err := NewWorkspace(cfg, "test-user", grant)
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}
	t.Cleanup(func() { workspace.Close() })
	return workspace
}
