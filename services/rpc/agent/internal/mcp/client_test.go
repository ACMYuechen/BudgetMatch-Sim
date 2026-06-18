package mcp

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClientCallsTool(t *testing.T) {
	cfg := fakeServerConfig(t)
	client := NewClient(cfg)

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	result, err := client.CallTool(context.Background(), "echo", map[string]any{
		"message": "hello mcp",
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if got := firstText(result); got != "Echo: hello mcp" {
		t.Fatalf("tool result mismatch, got %q", got)
	}
}

func TestProbe(t *testing.T) {
	probe, err := Probe(context.Background(), fakeServerConfig(t))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if probe.ToolCount != 1 || probe.ToolName != "echo" {
		t.Fatalf("unexpected probe metadata: %+v", probe)
	}
	if probe.Message != "Echo: budgetmatch agent mcp probe" {
		t.Fatalf("unexpected probe message: %q", probe.Message)
	}
}

func fakeServerConfig(t *testing.T) Config {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return Config{
		Enabled: true,
		Command: "python3",
		Args:    []string{filepath.Join(filepath.Dir(file), "testdata", "fake_mcp_server.py")},
		Timeout: 3000,
	}
}
