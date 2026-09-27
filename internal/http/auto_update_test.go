package http

import (
	"context"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/stack"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

type fakeAutoUpdates struct {
	actor  string
	writes int
}

func (f *fakeAutoUpdates) Get(context.Context, stack.StackID) (autoupdate.Status, error) {
	return autoupdate.Status{Policy: autoupdate.Policy{Expression: autoupdate.DefaultExpression}}, nil
}
func (f *fakeAutoUpdates) Update(_ context.Context, _ stack.StackID, _ autoupdate.PolicyUpdate, actor string) (autoupdate.Status, error) {
	f.actor = actor
	f.writes++
	return autoupdate.Status{}, nil
}
func (f *fakeAutoUpdates) Resume(_ context.Context, _ stack.StackID, _ int64, actor string) (autoupdate.Status, error) {
	f.actor = actor
	f.writes++
	return autoupdate.Status{}, nil
}
func TestAutoUpdatePolicyRequiresAuthAndCSRF(t *testing.T) {
	updates := &fakeAutoUpdates{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{AutoUpdates: updates})
	path := "/api/v1/stacks/s/auto-update"
	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(stdhttp.MethodGet, "http://porty.local"+path, nil))
	if unauth.Code != 401 {
		t.Fatalf("auth=%d", unauth.Code)
	}
	body := `{"enabled":true,"expression":"0 0 * * *","expectedRevision":0}`
	denied := doAuthenticatedRequest(handler, session, "", "PUT", path, body)
	if denied.Code != 403 || updates.writes != 0 {
		t.Fatal("CSRF bypass")
	}
	spoof := doAuthenticatedRequest(handler, session, csrf, "PUT", path, `{"enabled":true,"expression":"0 0 * * *","expectedRevision":0,"actor":"other"}`)
	if spoof.Code != 400 || updates.writes != 0 {
		t.Fatal("actor spoof accepted")
	}
	response := doAuthenticatedRequest(handler, session, csrf, "PUT", path, body)
	if response.Code != 200 || updates.actor == "" || updates.actor == "other" {
		t.Fatalf("update=%d actor=%s", response.Code, updates.actor)
	}
	resume := doAuthenticatedRequest(handler, session, csrf, "POST", path+"/resume", `{"expectedRevision":1}`)
	if resume.Code != 200 || updates.writes != 2 {
		t.Fatalf("resume=%d", resume.Code)
	}
}
