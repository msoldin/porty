package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogRedactsRequestSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	})
	handler := AssignRequestID(AccessLog(logger, next))
	request := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/stacks?token=query-secret", strings.NewReader(`{"secret":"body-secret"}`))
	request.RemoteAddr = "192.0.2.4:12345"
	request.Header.Set("Cookie", "porty_session=cookie-secret")
	request.Header.Set("Authorization", "Bearer header-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d", response.Code)
	}
	log := output.String()
	for _, secret := range []string{"query-secret", "cookie-secret", "body-secret", "header-secret"} {
		if strings.Contains(log, secret) {
			t.Fatalf("access log exposed %s: %s", secret, log)
		}
	}
	for _, expected := range []string{`"method":"POST"`, `"path":"/api/v1/stacks"`, `"status":201`, `"remote_ip":"192.0.2.4"`, `"request_id":"req_`, `"duration_ms":`} {
		if !strings.Contains(log, expected) {
			t.Fatalf("access log missing %s: %s", expected, log)
		}
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request ID header")
	}
}
