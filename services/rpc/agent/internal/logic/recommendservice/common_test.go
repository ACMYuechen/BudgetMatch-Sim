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
	wrapped := errors.Join(agent.ErrContextTooLarge, errors.New("estimated 9000 tokens"))
	if got := mapRecommendError(wrapped); !errors.Is(got, apperrors.AgentContextTooLarge) {
		t.Fatalf("mapRecommendError() = %v", got)
	}
}
