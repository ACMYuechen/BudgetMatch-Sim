package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestIntegrationEverythingServer 集成测试：连接真实的 @modelcontextprotocol/server-everything MCP Server 并进行探测。
// 需设置环境变量 AGENT_MCP_INTEGRATION=1 才会执行，默认跳过以避免依赖外部服务。
func TestIntegrationEverythingServer(t *testing.T) {
	if os.Getenv("AGENT_MCP_INTEGRATION") != "1" {
		t.Skip("set AGENT_MCP_INTEGRATION=1 to run against @modelcontextprotocol/server-everything")
	}

	cfg := Config{
		Enabled: true,
		Command: getenv("AGENT_MCP_COMMAND", "npx"),
		Args:    splitArgs(getenv("AGENT_MCP_ARGS", "-y @modelcontextprotocol/server-everything stdio")),
		Timeout: 15000,
	}

	probe, err := Probe(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if probe.ToolName != "echo" {
		t.Fatalf("expected echo tool, got %+v", probe)
	}
	if !strings.Contains(probe.Message, "Echo: budgetmatch agent mcp probe") {
		t.Fatalf("unexpected probe message: %q", probe.Message)
	}
}

// getenv 获取环境变量值，若不存在则返回默认值。
func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// splitArgs 将字符串按空白字符分割为参数列表；空字符串返回 nil。
func splitArgs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Fields(value)
}
