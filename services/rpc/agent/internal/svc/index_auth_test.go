package svc

import (
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/config"

	"github.com/stretchr/testify/require"
)

func TestRAGMissingIndexCredentialFailsBeforeExternalInitialization(t *testing.T) {
	var c config.Config
	c.MallRpc.Endpoints = []string{"unused.invalid:10005"}
	c.Database.DSN = "invalid-dsn-must-not-be-opened"
	c.Embedding.Provider = "invalid-provider-must-not-be-initialized"
	err := c.ValidateIndexAuth()
	require.Error(t, err)
	require.PanicsWithError(t, err.Error(), func() { NewServiceContext(c) })
}
