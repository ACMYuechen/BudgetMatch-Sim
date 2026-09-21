package svc

import (
	"context"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExplicitDemoExecutionWiringAndDisabledDefault(t *testing.T) {
	for _, mode := range []string{"", "demo"} {
		t.Run(mode, func(t *testing.T) {
			c := NewServiceContext(config.Config{DemandExecution: config.DemandExecutionConfig{Mode: mode}})
			_, err := c.RecommendService.PlanDemand(context.Background(), agent.Input{UserId: "u", ConversationId: "c", TurnId: "plan",
				Query: "desk", BudgetCents: 40000, MaxItems: 3}, `{"schema_version":1,"required":{"operation":"replace","values":["keyboard","mouse"]}}`)
			require.NoError(t, err)
			out, err := c.RecommendService.ExecuteDemand(context.Background(), "u", "c", "plan", "execute")
			if mode == "" {
				require.Equal(t, codes.FailedPrecondition, status.Code(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "complete", out.Status)
			require.Equal(t, "synthetic_demo_snapshot_only", out.Execution.Scope)
		})
	}
	invalid := config.Config{DemandExecution: config.DemandExecutionConfig{Mode: "live"}}
	invalid.Database.DSN = "must-not-open"
	require.PanicsWithError(t, "demand execution requires disabled, demo without Mall, or mall with Mall configured", func() { NewServiceContext(invalid) })
}

func TestDemandRAGInvalidWiringFailsBeforeExternalInitialization(t *testing.T) {
	for _, retrieval := range []string{"rga", "rag"} {
		c := config.Config{DemandExecution: config.DemandExecutionConfig{Mode: "mall", Retrieval: retrieval}}
		c.Database.DSN = "must-not-open"
		c.MallRpc.Endpoints = []string{"must-not-dial.invalid:10005"}
		err := c.ValidateDemandExecution()
		require.Error(t, err)
		require.PanicsWithError(t, err.Error(), func() { NewServiceContext(c) })
	}
	c := config.Config{DemandExecution: config.DemandExecutionConfig{Mode: "disabled", Retrieval: "rag"}}
	executor, err := newConfiguredDemandExecutor(c, nil, nil)
	require.NoError(t, err)
	require.Nil(t, executor)
}
