package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
)

func TestRepositorySetupStatusRequiresAuthenticationButNotReadyState(t *testing.T) {
	setup := notReadyRepositorySetup()
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup})
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://porty.local/api/v1/repository/setup/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	response := doAuthenticatedRequest(handler, session, "", http.MethodGet, "/api/v1/repository/setup/status", "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"required":true`)) {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
}

func TestRepositoryRemoteInspectionRequiresCSRFButNotReadyState(t *testing.T) {
	setup := notReadyRepositorySetup()
	setup.inspection = domain.RemoteInspection{RemoteURL: "https://example.com/repo.git", Branches: []string{"trunk"}, Suggested: "trunk"}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup})
	body := `{"remote":{"url":"https://user@example.com/repo.git","authentication":{"type":"none"}}}`
	denied := doAuthenticatedRequest(handler, session, "", http.MethodPost, "/api/v1/repository/setup/inspect-remote", body)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", denied.Code)
	}
	response := doAuthenticatedRequest(handler, session, csrf, http.MethodPost, "/api/v1/repository/setup/inspect-remote", body)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"suggestedBranch":"trunk"`)) {
		t.Fatalf("inspection = %d %s", response.Code, response.Body.String())
	}
}

func TestRepositorySetupAcceptsLocalOnlyRequest(t *testing.T) {
	setup := notReadyRepositorySetup()
	setup.status = readySetupStatus(nil)
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup})
	response := doAuthenticatedRequest(handler, session, csrf, http.MethodPost, "/api/v1/repository/setup", `{"mode":"init","branch":"main","author":{"name":"Porty","email":"porty@localhost"}}`)
	if response.Code != http.StatusCreated || setup.request.Mode != domain.RepositorySetupInit || setup.request.Remote != nil {
		t.Fatalf("setup = %d %s request=%#v", response.Code, response.Body.String(), setup.request)
	}
}

func TestRepositorySetupRejectsOversizedBody(t *testing.T) {
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: notReadyRepositorySetup()})
	body := `{"mode":"init","branch":"` + strings.Repeat("x", (1<<20)+1) + `"}`
	response := doAuthenticatedRequest(handler, session, csrf, http.MethodPost, "/api/v1/repository/setup", body)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestRepositorySetupNeverReturnsSubmittedSecret(t *testing.T) {
	setup := notReadyRepositorySetup()
	setup.status = readySetupStatus(&domain.RepositoryRemoteSummary{Name: "origin", URL: "https://example.com/repo.git", AuthType: domain.RepositoryAuthHTTPS, Managed: true})
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup})
	secret := "never-return-this-secret"
	body := `{"mode":"remote","branch":"main","author":{"name":"Porty","email":"porty@localhost"},"remote":{"url":"https://example.com/repo.git","authentication":{"type":"https","username":"git","secret":"` + secret + `"}}}`
	response := doAuthenticatedRequest(handler, session, csrf, http.MethodPost, "/api/v1/repository/setup", body)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), secret) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestRepositoryRemoteSettingsRequireReadyState(t *testing.T) {
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: notReadyRepositorySetup()})
	for _, test := range []struct{ method, body string }{{http.MethodPut, `{"remote":{"url":"https://example.com/repo.git","authentication":{"type":"none"}},"branch":"main"}`}, {http.MethodDelete, ""}} {
		response := doAuthenticatedRequest(handler, session, csrf, test.method, "/api/v1/repository/remote", test.body)
		assertAPIError(t, response, http.StatusConflict, "RepositorySetupRequired")
	}
}

func TestRepositorySetupErrorsUseStableCodes(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{application.ErrInvalidRequest, 400, "InvalidRequest"}, {application.ErrRepositoryPathNotEmpty, 409, "RepositoryPathNotEmpty"},
		{application.ErrInvalidWorktree, 409, "InvalidWorktree"}, {application.ErrDetachedHead, 409, "DetachedHead"},
		{application.ErrRemoteAuthenticationFailed, 401, "RemoteAuthenticationFailed"}, {application.ErrRemoteUnavailable, 502, "RemoteUnavailable"},
		{application.ErrSSHMaterialUnavailable, 409, "SSHMaterialUnavailable"}, {application.ErrUnrelatedHistory, 409, "UnrelatedHistory"},
		{application.ErrRepositoryRemoteConflict, 409, "RepositoryRemoteConflict"}, {application.ErrRepositoryRemoteUnavailable, 409, "RepositoryRemoteUnavailable"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			setup := notReadyRepositorySetup()
			setup.err = test.err
			handler, session, _ := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup})
			response := doAuthenticatedRequest(handler, session, "", http.MethodGet, "/api/v1/repository/setup/status", "")
			assertAPIError(t, response, test.status, test.code)
			if strings.Contains(response.Body.String(), test.err.Error()+":") {
				t.Fatalf("wrapped error leaked: %s", response.Body.String())
			}
		})
	}
}

func TestNormalReadRouteRequiresRepositorySetup(t *testing.T) {
	stacks := &fakeStackAPI{items: []domain.Stack{{ID: "stk_one", DirectoryName: "one"}}}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: notReadyRepositorySetup(), Stacks: stacks})
	response := doAuthenticatedRequest(handler, session, "", http.MethodGet, "/api/v1/stacks", "")
	assertAPIError(t, response, 409, "RepositorySetupRequired")
}

func TestNormalMutationRouteRequiresRepositorySetup(t *testing.T) {
	stacks := &fakeStackAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: notReadyRepositorySetup(), Stacks: stacks})
	response := doAuthenticatedRequest(handler, session, csrf, http.MethodPost, "/api/v1/stacks", `{"name":"blocked"}`)
	assertAPIError(t, response, 409, "RepositorySetupRequired")
	if stacks.created != "" {
		t.Fatalf("stack created before setup: %q", stacks.created)
	}
}

func TestRepositorySetupAuditContainsNoSecretOrRawRemoteURL(t *testing.T) {
	setup := notReadyRepositorySetup()
	setup.status = readySetupStatus(&domain.RepositoryRemoteSummary{Name: "origin", URL: "https://example.com/repo.git", AuthType: domain.RepositoryAuthHTTPS, Managed: true})
	audit := &fakeAuditAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup, Audit: audit})
	audit.recorded = nil
	secret, rawURL := "audit-secret", "https://git@example.com/repo.git"
	body := `{"mode":"remote","branch":"main","author":{"name":"Porty","email":"porty@localhost"},"remote":{"url":"` + rawURL + `","authentication":{"type":"https","username":"git","secret":"` + secret + `"}}}`
	response := doAuthenticatedRequest(handler, session, csrf, http.MethodPost, "/api/v1/repository/setup", body)
	if response.Code != http.StatusCreated || len(audit.recorded) != 1 {
		t.Fatalf("response/audit = %d/%#v", response.Code, audit.recorded)
	}
	encoded, _ := json.Marshal(audit.recorded[0])
	if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte(rawURL)) {
		t.Fatalf("audit leaked credentials: %s", encoded)
	}
	if audit.recorded[0].Action != "repository.setup.remote" || audit.recorded[0].TargetID != "branch=main;remote=https://example.com/repo.git" {
		t.Fatalf("audit = %#v", audit.recorded[0])
	}
	setup.err = application.ErrRemoteUnavailable
	audit.recorded = nil
	failed := doAuthenticatedRequest(handler, session, csrf, http.MethodPost, "/api/v1/repository/setup", body)
	if failed.Code != http.StatusBadGateway || len(audit.recorded) != 1 || !strings.Contains(audit.recorded[0].TargetID, "code=RemoteUnavailable") {
		t.Fatalf("failed audit = %d %#v", failed.Code, audit.recorded)
	}
}

func notReadyRepositorySetup() *fakeRepositorySetup {
	return &fakeRepositorySetup{readySet: true, status: domain.RepositorySetupStatus{State: domain.RepositorySetupRegistered, Required: true}}
}
func readySetupStatus(remote *domain.RepositoryRemoteSummary) domain.RepositorySetupStatus {
	return domain.RepositorySetupStatus{State: domain.RepositorySetupReady, Required: false, ManagedRemote: remote}
}
func doAuthenticatedRequest(handler http.Handler, session *http.Cookie, csrf, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://porty.local"+path, strings.NewReader(body))
	request.AddCookie(session)
	if method != http.MethodGet {
		request.Header.Set("Origin", "http://porty.local")
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var envelope ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error: %v (%s)", err, response.Body.String())
	}
	if response.Code != status || envelope.Error.Code != code {
		t.Fatalf("error = %d %#v", response.Code, envelope.Error)
	}
}
