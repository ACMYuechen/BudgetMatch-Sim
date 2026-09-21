package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budgetmatch-sim/cmd/app/internal/svc"
	"github.com/stretchr/testify/require"
)

func TestGeneratedStreamHandlerDelegatesToAuthenticatedHTTPBoundary(t *testing.T) {
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/recommend/stream", strings.NewReader(`{"query":"PRIVATE","stream_version":1}`))
	AgentRecommendStreamHandler(&svc.ServiceContext{})(response, req)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, response.Body.String(), "PRIVATE")
}
