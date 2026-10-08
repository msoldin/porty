package http

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msoldin/porty/internal/compose"
	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/stack"
)

func TestOnDemandRoutesExplainNativeHostFailureWithoutPrivateDetails(t *testing.T) {
	f := &fakeOnDemand{err: fmt.Errorf("%w: %w: /private/socket secret=value", compose.ErrOnDemandHostUnavailable, compose.ErrProtectionUnavailable)}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{OnDemand: f})
	response := doAuthenticatedRequest(handler, session, csrf, "POST", "/api/v1/stacks/s/on-demand", `{}`)
	body := response.Body.String()
	if response.Code != 409 || !strings.Contains(body, `"code":"OnDemandHostUnavailable"`) || !strings.Contains(body, "Docker Desktop") {
		t.Fatalf("missing actionable host explanation: %d %s", response.Code, body)
	}
	if strings.Contains(body, "private/socket") || strings.Contains(body, "secret=value") {
		t.Fatal("private host error details exposed")
	}
}

type fakeOnDemand struct {
	writes int
	err    error
}

func (f *fakeOnDemand) ListGroups(context.Context, stack.StackID) ([]ondemand.Group, error) {
	return []ondemand.Group{{ID: "g", Evidence: ondemand.Evidence{SourceDigest: "private-digest", ContainerIDs: []string{"private-id"}}}}, nil
}
func (f *fakeOnDemand) SaveGroup(context.Context, stack.StackID, string, ondemand.PolicyUpdate) (ondemand.Group, error) {
	f.writes++
	return ondemand.Group{}, f.err
}
func (f *fakeOnDemand) DeleteGroup(context.Context, stack.StackID, string, int64) error {
	f.writes++
	return f.err
}
func (f *fakeOnDemand) HoldGroup(context.Context, stack.StackID, string, int64) error {
	f.writes++
	return f.err
}
func (f *fakeOnDemand) ResumeGroup(context.Context, stack.StackID, string, int64) (ondemand.Group, error) {
	f.writes++
	return ondemand.Group{}, f.err
}
func TestOnDemandRoutesRequireAuthenticationAndCSRF(t *testing.T) {
	f := &fakeOnDemand{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{OnDemand: f})
	path := "/api/v1/stacks/s/on-demand"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "http://porty.local"+path, nil))
	if response.Code != 401 {
		t.Fatalf("unauthenticated: %d", response.Code)
	}
	read := doAuthenticatedRequest(handler, session, csrf, "GET", path, "")
	if read.Code != 200 || strings.Contains(read.Body.String(), "private-") {
		t.Fatal("private runtime identity exposed")
	}
	for _, action := range []struct{ method, path, body string }{{"POST", path, `{}`}, {"PUT", path + "/g", `{}`}, {"POST", path + "/g/hold", `{"expectedRevision":1}`}, {"POST", path + "/g/resume", `{"expectedRevision":1}`}, {"DELETE", path + "/g", `{"expectedRevision":1}`}} {
		denied := doAuthenticatedRequest(handler, session, "", action.method, action.path, action.body)
		if denied.Code != 403 {
			t.Fatalf("CSRF bypass: %s %d", action.path, denied.Code)
		}
	}
	if f.writes != 0 {
		t.Fatal("denied request mutated state")
	}
	f.err = ondemand.ErrConflict
	stale := doAuthenticatedRequest(handler, session, csrf, "POST", path+"/g/resume", `{"expectedRevision":1}`)
	if stale.Code != 409 {
		t.Fatalf("stale write: %d %s", stale.Code, stale.Body.String())
	}
	spoof := doAuthenticatedRequest(handler, session, csrf, "POST", path, `{"phase":"sleeping"}`)
	if spoof.Code != 400 {
		t.Fatal("runtime phase injection accepted")
	}
}
