//go:build linux

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"github.com/cloudwego/eino/components/tool"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// 只启动本地测试二进制，不连接外部 MCP、网络或模型。
func TestMCPHelperProcess(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "m13-helper=") {
			mode = strings.TrimPrefix(arg, "m13-helper=")
		}
	}
	if mode == "" {
		return
	}
	if mode == "hang" {
		for {
			time.Sleep(time.Second)
		}
	}
	s := server.NewMCPServer("test", "1", server.WithToolCapabilities(false))
	s.AddTool(readonlyTool("lookup"), func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		dir, _ := os.Getwd()
		data, _ := json.Marshal(map[string]string{"directory": dir, "inherited": os.Getenv("M13_TEST_PRIVATE_ENV")})
		return mcpgo.NewToolResultText(string(data)), nil
	})
	if err := server.ServeStdio(s); err != nil {
		os.Exit(2)
	}
	os.Exit(0) // 避免 testing 的 PASS 文本进入 stdio JSON-RPC。
}

func helperMCPConfig(t *testing.T, mode string) mcpconfig.Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return mcpconfig.Config{Enabled: true, Command: exe, Args: []string{"-test.run=^TestMCPHelperProcess$", "--", "m13-helper=" + mode}, AllowedTools: []string{"lookup"}, Timeout: 2000}
}

func TestMCPProcessEnvironmentDirectoryAndCleanup(t *testing.T) {
	t.Setenv("M13_TEST_PRIVATE_ENV", "not-inherited")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	loaded, cleanup, err := mcpTools(ctx, helperMCPConfig(t, "serve"), &session{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out, err := loaded[0].(tool.InvokableTool).InvokableRun(ctx, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var reported struct{ Directory, Inherited string }
	var envelope struct{ Content []struct{ Text string } }
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || len(envelope.Content) != 1 {
		t.Fatalf("helper envelope: %q %v", out, err)
	}
	if err := json.Unmarshal([]byte(envelope.Content[0].Text), &reported); err != nil {
		t.Fatalf("helper response: %q %v", out, err)
	}
	if reported.Inherited != "" || !strings.HasPrefix(filepath.Base(reported.Directory), "agent-mcp-") {
		t.Fatalf("unsafe child environment: %+v", reported)
	}
	info, err := os.Stat(reported.Directory)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("cwd mode: %v %v", info, err)
	}
	start := time.Now()
	cleanup()
	cleanup()
	if time.Since(start) > 2*time.Second {
		t.Fatal("cleanup blocked")
	}
	if _, err := os.Stat(reported.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cwd not cleaned: %v", err)
	}
}

func TestMCPInitializationTimeoutKillsNonCooperativeProcess(t *testing.T) {
	cfg := helperMCPConfig(t, "hang")
	cfg.Timeout = 100
	start := time.Now()
	loaded, cleanup, err := mcpTools(context.Background(), cfg, &session{})
	defer cleanup()
	if err == nil || len(loaded) != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v %v", loaded, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("non-cooperative process blocked cleanup")
	}
}
