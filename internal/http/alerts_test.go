package http

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/msoldin/porty/internal/alert"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
	stdhttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func alertFixture(t *testing.T) (*alert.Service, alert.Alert) {
	t.Helper()
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := portysqlite.NewAlertStore(db)
	items, err := store.Apply(context.Background(), []alert.Change{{Kind: "failure", Key: alert.Key{StackID: "deleted", Problem: "deployment", Target: "stack"}, StackName: "Archived app", OccurrenceID: "op1", Summary: "Deployment failed", ObservedAt: time.Now(), CanResolveManually: true}})
	if err != nil {
		t.Fatal(err)
	}
	return alert.NewService(store, nil), items[0]
}

func TestAlertMutationRequiresAuthenticatedOriginAndCSRF(t *testing.T) {
	service, a := alertFixture(t)
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Alerts: service, RepositorySetup: notReadyRepositorySetup()})
	path := "/api/v1/alerts/" + a.ID + "/acknowledge"
	for _, tc := range []struct {
		name, origin, token string
		cookie              bool
		status              int
	}{
		{"no session", "http://porty.local", csrf, false, 401},
		{"foreign origin", "http://evil.local", csrf, true, 403},
		{"no csrf", "http://porty.local", "", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://porty.local"+path, bytes.NewBufferString(`{"expectedRevision":1}`))
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-CSRF-Token", tc.token)
			if tc.cookie {
				r.AddCookie(session)
			}
			r.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: tc.token})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	response := doAuthenticatedRequest(handler, session, csrf, "POST", path, `{"expectedRevision":1}`)
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
}

func TestAlertActorComesFromSessionAndRejectsStaleRevision(t *testing.T) {
	service, a := alertFixture(t)
	audit := &fakeAuditAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Alerts: service, Audit: audit})
	audit.recorded = nil
	path := "/api/v1/alerts/" + a.ID + "/acknowledge"
	spoof := doAuthenticatedRequest(handler, session, csrf, "POST", path, `{"expectedRevision":1,"actorId":"other"}`)
	if spoof.Code != 400 {
		t.Fatalf("spoof status %d", spoof.Code)
	}
	response := doAuthenticatedRequest(handler, session, csrf, "POST", path, `{"expectedRevision":1}`)
	var updated alert.Alert
	json.Unmarshal(response.Body.Bytes(), &updated)
	if response.Code != 200 || updated.AcknowledgedBy == "" || len(audit.recorded) != 1 || audit.recorded[0].ActorUserID != updated.AcknowledgedBy {
		t.Fatalf("response=%s audit=%+v", response.Body.String(), audit.recorded)
	}
	stale := doAuthenticatedRequest(handler, session, csrf, "POST", path, `{"expectedRevision":1}`)
	if stale.Code != 409 {
		t.Fatalf("stale status %d", stale.Code)
	}
	if len(audit.recorded) != 1 {
		t.Fatal("failed change audited as successful")
	}
}

func TestAlertBadgeCountIgnoresPaginationAndRepositoryReadiness(t *testing.T) {
	service, a := alertFixture(t)
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Alerts: service, RepositorySetup: notReadyRepositorySetup()})
	response := doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/alerts?stackId=other&limit=1&offset=9", "")
	var page alert.Page
	json.Unmarshal(response.Body.Bytes(), &page)
	if response.Code != 200 || page.UnacknowledgedCount != 1 || len(page.Items) != 0 {
		t.Fatalf("page=%s", response.Body.String())
	}
	detail := doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/alerts/"+a.ID, "")
	if detail.Code != 200 || !bytes.Contains(detail.Body.Bytes(), []byte("Archived app")) || !bytes.Contains(detail.Body.Bytes(), []byte(`"history"`)) {
		t.Fatalf("detail=%s", detail.Body.String())
	}
	for _, query := range []string{"view=unknown", "limit=bad", "offset=-1", "limit=-3"} {
		response = doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/alerts?"+query, "")
		if response.Code != 400 {
			t.Fatalf("query %s status %d", query, response.Code)
		}
	}
	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest("GET", "http://porty.local/api/v1/alerts", nil))
	if unauth.Code != 401 {
		t.Fatalf("unauth %d", unauth.Code)
	}
}

func TestAlertResolveRemainsUnacknowledgedAndMissingAlertReturnsNotFound(t *testing.T) {
	service, a := alertFixture(t)
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Alerts: service})
	response := doAuthenticatedRequest(handler, session, csrf, "POST", "/api/v1/alerts/"+a.ID+"/resolve", `{"expectedRevision":1,"note":"Recovered manually"}`)
	var updated alert.Alert
	json.Unmarshal(response.Body.Bytes(), &updated)
	if response.Code != 200 || updated.ResolvedAt == nil || updated.AcknowledgedAt != nil || updated.ResolvedBy == "" {
		t.Fatalf("resolve=%s", response.Body.String())
	}
	missing := doAuthenticatedRequest(handler, session, "", "GET", "/api/v1/alerts/missing", "")
	if missing.Code != 404 {
		t.Fatalf("missing status=%d", missing.Code)
	}
}
