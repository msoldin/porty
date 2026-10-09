package http

import (
	"context"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyop "github.com/msoldin/porty/internal/operation"
	portystack "github.com/msoldin/porty/internal/stack"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDeploymentReview struct {
	calls    int
	revision string
	err      error
}

func (f *fakeDeploymentReview) ReviewDeployment(context.Context, portystack.StackID) (portycontrol.DeploymentReview, error) {
	f.calls++
	return portycontrol.DeploymentReview{StackID: "one", SourceRevision: strings.Repeat("a", 64)}, f.err
}
func (f *fakeDeploymentReview) StartReviewedDeployment(_ context.Context, _ portystack.StackID, revision string) (portyop.Operation, error) {
	f.calls++
	f.revision = revision
	return portyop.Operation{ID: "reviewed", Status: portyop.OperationQueued}, f.err
}
func TestDeploymentReviewHTTPContract(t *testing.T) {
	for _, tc := range []struct {
		name, header                               string
		status                                     int
		stale, missing, unauth, badOrigin, badCSRF bool
	}{
		{name: "legacy", status: 202}, {name: "reviewed", header: `"` + strings.Repeat("a", 64) + `"`, status: 202},
		{name: "stale", header: `"` + strings.Repeat("a", 64) + `"`, status: 412, stale: true},
		{name: "malformed", header: "bad", status: 400}, {name: "oversized", header: strings.Repeat("a", 4096), status: 400},
		{name: "missing capability", header: `"` + strings.Repeat("a", 64) + `"`, status: 503, missing: true},
		{name: "unauthenticated", header: `"` + strings.Repeat("a", 64) + `"`, status: 401, unauth: true},
		{name: "origin", header: `"` + strings.Repeat("a", 64) + `"`, status: 403, badOrigin: true},
		{name: "csrf", header: `"` + strings.Repeat("a", 64) + `"`, status: 403, badCSRF: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			review := &fakeDeploymentReview{}
			if tc.stale {
				review.err = portycontrol.ErrDeploymentReviewChanged
			}
			options := RouterOptions{Actions: &fakeActionAPI{}, DeploymentReview: review}
			if tc.missing {
				options.DeploymentReview = nil
			}
			handler, session, csrf := authenticatedAPIRouter(t, options)
			r := httptest.NewRequest("POST", "http://porty.local/api/v1/stacks/one/actions/deploy", nil)
			if tc.header != "" {
				r.Header.Set("If-Match", tc.header)
			}
			r.Header.Set("Origin", "http://porty.local")
			if tc.badOrigin {
				r.Header.Set("Origin", "http://foreign.local")
			}
			r.Header.Set("X-CSRF-Token", csrf)
			if tc.badCSRF {
				r.Header.Set("X-CSRF-Token", "bad")
			}
			r.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
			if !tc.unauth {
				r.AddCookie(session)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.stale {
				assertAPIError(t, response, 412, "DeploymentReviewChanged")
			}
			if tc.name == "reviewed" && review.revision != strings.Repeat("a", 64) {
				t.Fatal("revision not forwarded")
			}
			if (tc.status == 400 || tc.status == 401 || tc.status == 403 || tc.status == 503 || tc.name == "legacy") && review.calls != 0 {
				t.Fatal("invalid request reached review capability")
			}
		})
	}
}
func TestDeploymentReviewReadRequiresSession(t *testing.T) {
	review := &fakeDeploymentReview{}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{DeploymentReview: review})
	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest("GET", "http://porty.local/api/v1/stacks/one/deployment-review", nil)
		if authenticated {
			r.AddCookie(session)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		want := 401
		if authenticated {
			want = 200
		}
		if response.Code != want {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if review.calls != 1 {
		t.Fatalf("review calls=%d", review.calls)
	}
}
