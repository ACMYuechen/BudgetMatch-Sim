package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budgetmatch-sim/cmd/app/internal/svc"
	"github.com/go-playground/validator/v10"
)

func TestDemandPlanHandlerRejectsMalformedInputWithoutEcho(t *testing.T) {
	handler := AgentDemandPlanHandler(&svc.ServiceContext{Validator: validator.New()})
	for _, body := range []string{
		`{"query":"PRIVATE", "demand_patch": {"PRIVATE":"not a string"}}`,
		`{"query":"PRIVATE", "max_items":"PRIVATE"}`,
		`{"query":"PRIVATE", "max_items":11}`,
		`{"query":"PRIVATE", "demand_patch":"` + strings.Repeat("PRIVATE", 2000) + `"}`,
		`{"query":"PRIVATE",`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/intent/plan", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code < 400 || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatalf("unsafe error: %d %s", w.Code, w.Body.String())
		}
	}
}
