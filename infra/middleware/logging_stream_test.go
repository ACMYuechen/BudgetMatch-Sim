package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingMiddlewareFlushesStreamingResponse(test *testing.T) {
	recorder := httptest.NewRecorder()
	handler := NewLoggingMiddleware("").Handle(func(writer http.ResponseWriter, incoming *http.Request) {
		flusher, ok := writer.(http.Flusher)
		if !ok {
			test.Fatal("logging middleware removed streaming support")
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: ready\ndata: {}\n\n"))
		flusher.Flush()
		if !recorder.Flushed {
			test.Error("event was not flushed before handler completed")
		}
	})
	handler(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "event: ready\ndata: {}\n\n" {
		test.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func TestLoggingMiddlewarePreservesMissingFlusher(test *testing.T) {
	writer := struct{ http.ResponseWriter }{httptest.NewRecorder()}
	handler := NewLoggingMiddleware("").Handle(func(writer http.ResponseWriter, incoming *http.Request) {
		if _, ok := writer.(http.Flusher); ok {
			test.Error("writer incorrectly advertises streaming support")
		}
	})
	handler(writer, httptest.NewRequest(http.MethodGet, "/plain", nil))
}
