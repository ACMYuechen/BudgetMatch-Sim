package agent

import (
	"net/http/httptest"
	"strings"
	"testing"

	agentlogic "budgetmatch-sim/cmd/app/internal/logic/agent"
)

func TestWriteSSE(t *testing.T) {
	recorder := httptest.NewRecorder()

	err := writeSSE(recorder, recorder, agentlogic.StreamEvent{
		Event: "recommendation.final",
		Data: map[string]any{
			"summary": "ok",
		},
	})
	if err != nil {
		t.Fatalf("writeSSE() error = %v", err)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"event: recommendation.final\n",
		`data: {"summary":"ok"}`,
		"\n\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}
