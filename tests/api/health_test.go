package api_test

import (
	"net/http"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	response := newClient().request(t, http.MethodGet, "/health", nil)
	assertStatus(t, response, http.StatusOK)
	if !containsJSONContentType(response.Header) {
		t.Fatalf("expected JSON Content-Type, got %q", response.Header.Get("Content-Type"))
	}

	var body struct {
		Status string `json:"status"`
	}
	response.decode(t, &body)
	if body.Status != "ok" {
		t.Fatalf("expected healthy status, got %q", body.Status)
	}
}
