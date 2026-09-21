package svc

import (
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/config"
	"budgetmatch-sim/services/rpc/agent/internal/model"
	"github.com/stretchr/testify/require"
)

func TestInvalidModelConfigStopsBeforeExternalInitialization(t *testing.T) {
	for _, modelCfg := range []model.Config{
		{Provider: "openai", Model: "deepseek-flash", APIKey: "fixture-key"},
		{Provider: "openai", Model: "deepseek-flash", Thinking: "enabled", APIKey: "fixture-key"},
		{Provider: "openai", Model: "local-fixture", Thinking: "disabled", APIKey: "fixture-key"},
		{Provider: "openai"},
		{Provider: "unsupported-provider"},
	} {
		t.Run(modelCfg.Provider+"/"+modelCfg.Model+"/"+modelCfg.Thinking, func(t *testing.T) {
			cfg := config.Config{Model: modelCfg}
			cfg.Database.DSN = "invalid-dsn-must-not-be-opened"
			cfg.Database.Driver = "invalid-driver-must-not-be-initialized"
			err := cfg.Model.Validate()
			require.Error(t, err)
			require.PanicsWithError(t, err.Error(), func() { NewServiceContext(cfg) })
		})
	}
}
