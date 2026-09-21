package mcp

import (
	"math"
	"testing"
	"time"
)

func TestMCPConfigPolicy(t *testing.T) {
	for _, names := range [][]string{{"*"}, {"lookup", "lookup"}, {"write_file"}, {"select_bundle"}, {"../lookup"}, {"private\nname"}} {
		if err := (Config{Enabled: true, Command: "/opt/mcp", AllowedTools: names}).Validate(); err == nil {
			t.Fatalf("unsafe list: %v", names)
		}
	}
	if err := (Config{Enabled: true, Command: "npx", AllowedTools: []string{"lookup"}}).Validate(); err == nil {
		t.Fatal("relative executable allowed")
	}
	if err := (Config{Enabled: true, Command: "/opt/mcp", AllowedTools: []string{"lookup"}}).Validate(); err != nil {
		t.Fatal(err)
	}
	if (Config{Enabled: true}).Ready() {
		t.Fatal("empty allowlist ready")
	}
	if (Config{Timeout: math.MaxInt64}).RequestTimeout() != 30*time.Second {
		t.Fatal("timeout overflow")
	}
	if (Config{}).RequestTimeout() != 5*time.Second {
		t.Fatal("missing default timeout")
	}
}
