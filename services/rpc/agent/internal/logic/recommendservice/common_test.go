package recommendservicelogic

import (
	"context"
	"errors"
	"testing"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/agent/internal/agent"
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

func TestMapRecommendError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "invalid input", err: agent.ErrInvalidInput, want: apperrors.Invalid},
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
