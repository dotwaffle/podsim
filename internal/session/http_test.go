package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	handler := newTestSession(t).Handler(t.TempDir())
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", http.NoBody)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
		t.Fatalf("health response: status %d body %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health headers: %v", response.Header())
	}
}
