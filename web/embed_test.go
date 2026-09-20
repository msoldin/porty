package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedApplicationShell(t *testing.T) {
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Porty") {
		t.Fatalf("body does not contain Porty: %q", response.Body.String())
	}
}

func TestHandlerServesProductionJavaScript(t *testing.T) {
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	match := regexp.MustCompile(`src="([^"]+\.js)"`).FindStringSubmatch(response.Body.String())
	if len(match) != 2 {
		t.Fatal("application shell has no production JavaScript")
	}
	asset := httptest.NewRecorder()
	Handler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, match[1], nil))
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("asset status/type: %d %s", asset.Code, asset.Header().Get("Content-Type"))
	}
}
