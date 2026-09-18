package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budgetmatch-sim/cmd/app/internal/svc"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
)

func TestDemandExecuteHandlerRejectsMalformedInputWithoutEcho(t *testing.T) {
	handler := AgentDemandExecuteHandler(&svc.ServiceContext{Validator: validator.New()})
	for _, body := range []string{`{"conversation_id":"PRIVATE"}`, `{"plan_turn_id":"PRIVATE"}`,
		`{"conversation_id":"c","plan_turn_id":{"PRIVATE":true}}`, `{"PRIVATE":`,
		`{"conversation_id":"c","plan_turn_id":"` + strings.Repeat("PRIVATE", 30) + `"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/intent/execute", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, req)
		require.GreaterOrEqual(t, w.Code, 400)
		require.NotContains(t, w.Body.String(), "PRIVATE")
	}
}
