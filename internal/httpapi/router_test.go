package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndReadinessAreSeparateContracts(t *testing.T) {
	mux := NewMux(func(context.Context) error { return errors.New("docker unavailable") })

	health := httptest.NewRecorder()
	mux.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok\n" {
		t.Fatalf("health response = %d %q, want 200 ok", health.Code, health.Body.String())
	}

	ready := httptest.NewRecorder()
	mux.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d, want 503", ready.Code)
	}
	var body ErrorResponse
	if err := json.Unmarshal(ready.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v", err)
	}
	if body.Error.Code != "NotReady" || body.Error.Message != "Porty is not ready" {
		t.Fatalf("ready error = %#v", body.Error)
	}
}

func TestWriteErrorUsesSafeEnvelopeAndRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stacks", nil)
	req = req.WithContext(WithRequestID(req.Context(), "req_test"))
	response := httptest.NewRecorder()

	WriteError(response, req, http.StatusBadRequest, "InvalidPath", "The file path is invalid", map[string]any{"field": "path"})

	var got ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if got.Error.Code != "InvalidPath" || got.Error.RequestID != "req_test" || got.Error.Details["field"] != "path" {
		t.Fatalf("error response = %#v", got)
	}
}
