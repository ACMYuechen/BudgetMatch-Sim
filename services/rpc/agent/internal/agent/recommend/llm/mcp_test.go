package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	mcpconfig "budgetmatch-sim/services/rpc/agent/internal/mcp"
	"github.com/cloudwego/eino/components/tool"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeMCP struct {
	mcpclient.MCPClient
	pages        []*mcpgo.ListToolsResult
	lists, calls int
	result       *mcpgo.CallToolResult
	err          error
	request      mcpgo.CallToolRequest
	wait         bool
}

func (f *fakeMCP) ListToolsByPage(ctx context.Context, _ mcpgo.ListToolsRequest) (*mcpgo.ListToolsResult, error) {
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	i := f.lists
	f.lists++
	if i >= len(f.pages) {
		return nil, errors.New("unexpected listing")
	}
	return f.pages[i], f.err
}
func (f *fakeMCP) CallTool(ctx context.Context, r mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	f.calls++
	f.request = r
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.result, f.err
}
func readonlyTool(name string) mcpgo.Tool {
	return mcpgo.NewTool(name, mcpgo.WithReadOnlyHintAnnotation(true))
}
func policyFor(f *fakeMCP) *policyMCPClient {
	return &policyMCPClient{MCPClient: f, timeout: 20 * time.Millisecond, allowed: map[string]bool{"lookup": true}, validated: map[string]bool{}}
}

func TestMCPDiscoveryFiltersAndNamespaces(t *testing.T) {
	f := &fakeMCP{pages: []*mcpgo.ListToolsResult{{Tools: []mcpgo.Tool{readonlyTool("lookup"), readonlyTool("not_allowed")}}}, result: mcpgo.NewToolResultText("data")}
	s := &session{}
	loaded, err := loadMCPTools(context.Background(), mcpconfig.Config{Enabled: true, Command: "/audited/server", AllowedTools: []string{"lookup"}}, f, s)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load: %v %v", loaded, err)
	}
	info, err := loaded[0].Info(context.Background())
	if err != nil || info.Name != "mcp_lookup" {
		t.Fatalf("name: %v %v", info, err)
	}
	out, err := loaded[0].(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err != nil || out == "" || f.calls != 1 || f.request.Params.Name != "lookup" {
		t.Fatalf("call: %q %v %+v", out, err, f.request)
	}
	_, _, calls := s.snapshot()
	if len(calls) != 1 || strings.Contains(calls[0].Detail, "data") {
		t.Fatal(calls)
	}
}

func TestMCPDiscoveryFailsClosed(t *testing.T) {
	loop := &mcpgo.ListToolsResult{Tools: []mcpgo.Tool{readonlyTool("ignored")}}
	loop.NextCursor = "repeat"
	for _, tc := range []struct {
		name  string
		pages []*mcpgo.ListToolsResult
	}{
		{"missing", []*mcpgo.ListToolsResult{{}}},
		{"unannotated", []*mcpgo.ListToolsResult{{Tools: []mcpgo.Tool{mcpgo.NewTool("lookup")}}}},
		{"write", []*mcpgo.ListToolsResult{{Tools: []mcpgo.Tool{mcpgo.NewTool("lookup", mcpgo.WithReadOnlyHintAnnotation(false))}}}},
		{"duplicate", []*mcpgo.ListToolsResult{{Tools: []mcpgo.Tool{readonlyTool("lookup"), readonlyTool("lookup")}}}},
		{"nil", []*mcpgo.ListToolsResult{nil}},
		{"loop", []*mcpgo.ListToolsResult{loop, loop}},
		{"excess", []*mcpgo.ListToolsResult{{Tools: make([]mcpgo.Tool, 129)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := policyFor(&fakeMCP{pages: tc.pages})
			if _, err := p.ListTools(context.Background(), mcpgo.ListToolsRequest{}); err == nil {
				t.Fatal("invalid discovery allowed")
			}
			r := mcpgo.CallToolRequest{}
			r.Params.Name = "lookup"
			if _, err := p.CallTool(context.Background(), r); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("unvalidated call: %v", err)
			}
		})
	}
}

func TestMCPPaginationAndCallBoundary(t *testing.T) {
	first := &mcpgo.ListToolsResult{}
	first.NextCursor = "page2"
	f := &fakeMCP{pages: []*mcpgo.ListToolsResult{first, {Tools: []mcpgo.Tool{readonlyTool("lookup")}}}, result: mcpgo.NewToolResultText("ok")}
	p := policyFor(f)
	if _, err := p.ListTools(context.Background(), mcpgo.ListToolsRequest{}); err != nil {
		t.Fatal(err)
	}
	r := mcpgo.CallToolRequest{}
	r.Params.Name = "unapproved"
	if _, err := p.CallTool(context.Background(), r); status.Code(err) != codes.PermissionDenied || f.calls != 0 {
		t.Fatalf("unapproved: %v", err)
	}
	r.Params.Name = "lookup"
	r.Header = http.Header{"Authorization": []string{"private"}}
	r.Params.Meta = &mcpgo.Meta{AdditionalFields: map[string]any{"private": "secret"}}
	if _, err := p.CallTool(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if f.request.Header != nil || f.request.Params.Meta != nil {
		t.Fatal("forwarded authentication metadata")
	}
	r.Params.Arguments = map[string]string{"oversize": strings.Repeat("x", maxToolPayloadBytes)}
	if _, err := p.CallTool(context.Background(), r); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
}

func TestMCPCallErrorsTimeoutAndSizeAreSafe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *mcpgo.CallToolResult
		err    error
		wait   bool
	}{
		{"raw error", nil, errors.New("PRIVATE_PAYLOAD"), false},
		{"error result", mcpgo.NewToolResultError("PRIVATE_PAYLOAD"), nil, false},
		{"nil result", nil, nil, false},
		{"large", mcpgo.NewToolResultText(strings.Repeat("PRIVATE_PAYLOAD", maxToolPayloadBytes)), nil, false},
		{"timeout", nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMCP{result: tc.result, err: tc.err, wait: tc.wait}
			p := policyFor(f)
			p.validated["lookup"] = true
			r := mcpgo.CallToolRequest{}
			r.Params.Name = "lookup"
			out, err := p.CallTool(context.Background(), r)
			if out != nil || err == nil || strings.Contains(err.Error(), "PRIVATE_PAYLOAD") {
				t.Fatalf("unsafe result: %v %v", out, err)
			}
			if tc.wait && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
	p := policyFor(&fakeMCP{wait: true})
	if _, err := p.ListTools(context.Background(), mcpgo.ListToolsRequest{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
