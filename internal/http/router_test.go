package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/msoldin/porty/internal/http/middleware"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndReadinessAreSeparateContracts(t *testing.T) {
	mux := NewMux(func(context.Context) error { return errors.New("docker unavailable") })

	health := httptest.NewRecorder()
	mux.ServeHTTP(health, httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil))
	if health.Code != stdhttp.StatusOK || health.Body.String() != "ok\n" {
		t.Fatalf("health response = %d %q, want 200 ok", health.Code, health.Body.String())
	}

	ready := httptest.NewRecorder()
	mux.ServeHTTP(ready, httptest.NewRequest(stdhttp.MethodGet, "/readyz", nil))
	if ready.Code != stdhttp.StatusServiceUnavailable {
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
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/stacks", nil)
	req = req.WithContext(middleware.WithRequestID(req.Context(), "req_test"))
	response := httptest.NewRecorder()

	WriteError(response, req, stdhttp.StatusBadRequest, "InvalidPath", "The file path is invalid", map[string]any{"field": "path"})

	var got ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if response.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if got.Error.Code != "InvalidPath" || got.Error.RequestID != "req_test" || got.Error.Details["field"] != "path" {
		t.Fatalf("error response = %#v", got)
	}
}
