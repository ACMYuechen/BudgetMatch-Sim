package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"budgetmatch-sim/infra/buildinfo"
)

func TestHealthReturnsBuildCommitWithoutDependencies(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	HealthHandler(nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if len(response) != 2 || response["status"] != "OK" || response["commit"] != buildinfo.Commit() || response["commit"] == "" {
		t.Fatalf("unexpected health response: %v", response)
	}
}
