package http

import (
	"context"
	"encoding/json"
	"github.com/msoldin/porty/internal/monitoring"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type monitoringProbe struct{ calls atomic.Int32 }

func (p *monitoringProbe) ID() string { return "probe" }
func (p *monitoringProbe) Collect(context.Context) (monitoring.Batch, error) {
	p.calls.Add(1)
	return monitoring.Batch{}, nil
}
func (p *monitoringProbe) Close() error { return nil }
func testMonitoring(t *testing.T) *monitoring.Service {
	t.Helper()
	service, err := monitoring.NewService(monitoring.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Shutdown(context.Background()) })
	return service
}
func TestMonitoringRequiresAuthentication(t *testing.T) {
	handler, _, _ := authenticatedAPIRouter(t, RouterOptions{Monitoring: testMonitoring(t)})
	for _, token := range []string{"", "expired-token"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://porty.local/api/v1/monitoring", nil)
		if token != "" {
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		handler.ServeHTTP(response, request)
		if response.Code != 401 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code, response.Header())
		}
	}
}
func TestMonitoringWorksBeforeRepositorySetup(t *testing.T) {
	setup := notReadyRepositorySetup()
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup, Monitoring: testMonitoring(t)})
	response := doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/monitoring", "")
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(response.Code, response.Body.String())
	}
}
func TestMonitoringReadsCachedSamplesWithoutCollection(t *testing.T) {
	probe := &monitoringProbe{}
	service, err := monitoring.NewService(monitoring.ServiceOptions{Sources: []monitoring.Source{probe}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Monitoring: service})
	before := probe.calls.Load()
	for i := 0; i < 3; i++ {
		response := doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/monitoring", "")
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
		var got monitoring.Snapshot
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Generation == "" || got.Cursor == "" {
			t.Fatal("missing coherent cursor")
		}
	}
	if probe.calls.Load() != before {
		t.Fatal("HTTP request collected host metrics")
	}
}
func TestMonitoringRejectsMalformedCursor(t *testing.T) {
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Monitoring: testMonitoring(t)})
	for _, query := range []string{"cursor=bad", "cursor=" + strings.Repeat("a", 300), "cursor=x&cursor=y", "extra=value", "cursor=%zz"} {
		response := doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/monitoring?"+query, "")
		if response.Code != 400 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(query, response.Code)
		}
	}
}

type excessiveMonitoring struct{}

func (excessiveMonitoring) Snapshot(string) (monitoring.Snapshot, error) {
	return monitoring.Snapshot{Host: monitoring.HostInfo{Name: strings.Repeat("x", 8<<20)}}, nil
}
func TestMonitoringResponseIsNoStoreAndBounded(t *testing.T) {
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Monitoring: excessiveMonitoring{}})
	response := doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/monitoring", "")
	if response.Code != 500 || response.Body.Len() > 8<<20 || response.Header().Get("Cache-Control") != "no-store" || !json.Valid(response.Body.Bytes()) {
		t.Fatal(response.Code, response.Body.Len())
	}
}
