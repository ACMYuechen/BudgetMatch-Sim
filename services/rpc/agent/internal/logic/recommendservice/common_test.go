package recommendservicelogic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAuthenticatedUserIdOnlyUsesInterceptorContext(t *testing.T) {
	if _, err := authenticatedUserId(context.Background()); !errors.Is(err, apperrors.Unauthorized) {
		t.Fatalf("missing identity error = %v, want Unauthorized", err)
	}
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "trusted-user")
	userId, err := authenticatedUserId(ctx)
	if err != nil || userId != "trusted-user" {
		t.Fatalf("authenticatedUserId() = %q, %v", userId, err)
	}
	// 同名 string key 不能伪造拦截器私有 key。
	spoofed := context.WithValue(context.Background(), "user_id", "attacker")
	if _, err := authenticatedUserId(spoofed); !errors.Is(err, apperrors.Unauthorized) {
		t.Fatalf("spoofed identity error = %v, want Unauthorized", err)
	}
}

func TestResponseAndHistoricalToolDetailsAreRedacted(t *testing.T) {
	r := agent.Result{ToolsUsed: []agent.ToolCall{{Name: "tool.read_file", Success: true, Detail: `{"content":"PRIVATE"}`}, {Name: "tool.PRIVATE", Detail: "PRIVATE"}}}
	if got := fmt.Sprint(toPB(&r).ToolsUsed); strings.Contains(got, "PRIVATE") {
		t.Fatal(got)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := toPBTurn(memory.Turn{ResultJSON: data})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(turn.Result.ToolsUsed); strings.Contains(got, "PRIVATE") {
		t.Fatal(got)
	}
	err = mapRecommendError(status.Error(codes.PermissionDenied, "PRIVATE"))
	if status.Code(err) != codes.PermissionDenied || strings.Contains(status.Convert(err).Message(), "PRIVATE") {
		t.Fatal(err)
	}
}

func TestMapRecommendError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "invalid input", err: agent.ErrInvalidInput, want: apperrors.Invalid},
		{name: "budget currency", err: agent.ErrBudgetCurrency, want: apperrors.AgentBudgetCurrency},
		{name: "budget text", err: errors.Join(agent.ErrBudgetText, errors.New("private query")), want: apperrors.AgentBudgetText},
		{name: "item limit text", err: agent.ErrItemLimitText, want: apperrors.AgentItemLimitText},
		{name: "unsafe result", err: errors.Join(agent.ErrUnsafeResult, errors.New("private candidate detail")), want: apperrors.Internal},
		{name: "context too large", err: errors.Join(agent.ErrContextTooLarge, errors.New("estimated 9000 tokens")), want: apperrors.AgentContextTooLarge},
		{name: "turn conflict", err: agent.ErrTurnConflict, want: apperrors.AgentTurnConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mapRecommendError(test.err); !errors.Is(got, test.want) {
				t.Fatalf("mapRecommendError() = %v, want %v", got, test.want)
			}
		})
	}
}
