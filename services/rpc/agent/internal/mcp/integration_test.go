package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
)

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

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitArgs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Fields(value)
}
