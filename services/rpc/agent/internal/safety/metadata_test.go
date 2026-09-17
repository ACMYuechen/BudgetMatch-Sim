package safety

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProtectedErrorsKeepSemanticsWithoutPayload(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, agent.ErrBudgetText, agent.ErrTurnConflict, status.Error(codes.PermissionDenied, "PRIVATE"), errors.New("PRIVATE")} {
		err := Protect(fmt.Errorf("PRIVATE: %w", cause))
		wantCode := status.Code(cause)
		if errors.Is(cause, context.Canceled) {
			wantCode = codes.Canceled
		}
		if errors.Is(cause, context.DeadlineExceeded) {
			wantCode = codes.DeadlineExceeded
		}
		if strings.Contains(err.Error(), "PRIVATE") || !errors.Is(err, cause) || status.Code(err) != wantCode {
			t.Fatalf("changed semantics: %v -> %v", cause, err)
		}
		if strings.Contains(status.Convert(err).Message(), "PRIVATE") {
			t.Fatal("RPC error leaked")
		}
	}
	if Protect(nil) != nil {
		t.Fatal("nil became error")
	}
}

func TestProtectedRPCErrorDropsUpstreamDetails(t *testing.T) {
	upstream, err := status.New(codes.PermissionDenied, "PRIVATE").WithDetails(&errdetails.DebugInfo{Detail: "PRIVATE", StackEntries: []string{"PRIVATE"}})
	if err != nil {
		t.Fatal(err)
	}
	safe := status.Convert(Protect(upstream.Err()))
	if safe.Code() != codes.PermissionDenied || len(safe.Details()) != 0 || strings.Contains(safe.Message(), "PRIVATE") {
		t.Fatal(safe)
	}
}

func TestToolCallsSanitizeLegacyResultsIdempotently(t *testing.T) {
	calls := []agent.ToolCall{
		{Name: "tool.read_file", Success: true, Detail: `{"content":"PRIVATE"}`},
		{Name: "tool.PRIVATE", Detail: "PRIVATE"},
		{Name: "tool.search_products", Success: true, Detail: "status=ok output_bytes=12 duration_ms=1"},
		{Name: "tool.write_file", Detail: "error_code=PRIVATE duration_ms=1"},
	}
	got := ToolCalls(calls)
	if strings.Contains(fmt.Sprint(got), "PRIVATE") {
		t.Fatal(got)
	}
	if !strings.Contains(calls[0].Detail, "PRIVATE") {
		t.Fatal("mutated source")
	}
	if got[2] != calls[2] || !reflect.DeepEqual(got, ToolCalls(got)) {
		t.Fatal("lost safe metadata or not idempotent")
	}
}
