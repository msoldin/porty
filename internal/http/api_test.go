package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	portyrepo "github.com/msoldin/porty/internal/repository"
	stdhttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	portyauth "github.com/msoldin/porty/internal/auth"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyfs "github.com/msoldin/porty/internal/filesystem"
	portyop "github.com/msoldin/porty/internal/operation"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
	portystack "github.com/msoldin/porty/internal/stack"
)

func TestEnvironmentReadRequiresSessionAndReturnsOnlyRequestedValue(t *testing.T) {
	audit := &fakeAuditAPI{}
	environment := &fakeEnvironmentAPI{values: map[string]string{"TOKEN": "secret", "EMPTY": ""}}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Environment: environment, Audit: audit})
	get := func(path string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(stdhttp.MethodGet, "http://porty.local"+path, nil)
		if authenticated {
			request.AddCookie(session)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	base := "/api/v1/stacks/stk_gateway/environment"
	if got := get(base+"/TOKEN", false); got.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d", got.Code)
	}
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{base + "/TOKEN", 200, `{"value":"secret"}` + "\n"},
		{base + "/EMPTY", 200, `{"value":""}` + "\n"},
		{base + "/MISSING", 404, ""},
		{"/api/v1/stacks/stk_missing/environment/TOKEN", 404, ""},
		{base + "/BAD-KEY", 400, ""},
	} {
		got := get(tc.path, true)
		if got.Code != tc.status || got.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET %s = %d, cache=%q, body=%s", tc.path, got.Code, got.Header().Get("Cache-Control"), got.Body.String())
		}
		if tc.body != "" && got.Body.String() != tc.body {
			t.Fatalf("GET %s body = %q", tc.path, got.Body.String())
		}
		if tc.body == "" && strings.Contains(got.Body.String(), "secret") {
			t.Fatalf("GET %s leaked value", tc.path)
		}
	}
	listed := get(base, true)
	if listed.Code != 200 || !bytes.Contains(listed.Body.Bytes(), []byte(`"TOKEN"`)) || bytes.Contains(listed.Body.Bytes(), []byte("secret")) {
		t.Fatalf("unsafe list: %d %s", listed.Code, listed.Body.String())
	}
	for _, event := range audit.events {
		if strings.Contains(event.Action+event.TargetID+event.Outcome, "secret") {
			t.Fatal("audit leaked value")
		}
	}
}

type fakeEnvironmentAPI struct{ values map[string]string }

func (f *fakeEnvironmentAPI) EnvironmentKeys(_ context.Context, id portystack.StackID) ([]string, error) {
	if id != "stk_gateway" {
		return nil, sql.ErrNoRows
	}
	return []string{"EMPTY", "TOKEN"}, nil
}
func (f *fakeEnvironmentAPI) EnvironmentValue(_ context.Context, id portystack.StackID, key string) (string, error) {
	if strings.Contains(key, "-") {
		return "", portystack.ErrInvalidEnvironment
	}
	if id != "stk_gateway" {
		return "", sql.ErrNoRows
	}
	value, ok := f.values[key]
	if !ok {
		return "", sql.ErrNoRows
	}
	return value, nil
}
func (f *fakeEnvironmentAPI) SetEnvironment(context.Context, portystack.StackID, string, string) error {
	return nil
}
func (f *fakeEnvironmentAPI) DeleteEnvironment(context.Context, portystack.StackID, string) error {
	return nil
}

func TestStackEndpointsRequireSessionAndMutationsRequireCSRF(t *testing.T) {
	stacks := &fakeStackAPI{items: []portystack.Stack{{ID: "stk_gateway", DirectoryName: "gateway"}}}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{Stacks: stacks})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(stdhttp.MethodGet, "http://porty.local/api/v1/stacks", nil))
	if unauthorized.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	listRequest := httptest.NewRequest(stdhttp.MethodGet, "http://porty.local/api/v1/stacks", nil)
	listRequest.AddCookie(sessionCookie)
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, listRequest)
	if listed.Code != stdhttp.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte("gateway")) {
		t.Fatalf("list response = %d %s", listed.Code, listed.Body.String())
	}

	create := httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/stacks", bytes.NewBufferString(`{"name":"worker"}`))
	create.Header.Set("Origin", "http://porty.local")
	create.AddCookie(sessionCookie)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, create)
	if denied.Code != stdhttp.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", denied.Code)
	}

	create = httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/stacks", bytes.NewBufferString(`{"name":"worker"}`))
	create.Header.Set("Origin", "http://porty.local")
	create.Header.Set("X-CSRF-Token", csrf)
	create.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	create.AddCookie(sessionCookie)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != stdhttp.StatusCreated || stacks.created != "worker" {
		t.Fatalf("create response = %d %s", created.Code, created.Body.String())
	}
}

func TestDatabaseErrorDoesNotLeakToHTTPResponse(t *testing.T) {
	stacks := &fakeStackAPI{err: errors.New("sqlite: private-path-and-secret")}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Stacks: stacks})
	request := httptest.NewRequest(stdhttp.MethodGet, "http://porty.local/api/v1/stacks", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusInternalServerError || !bytes.Contains(response.Body.Bytes(), []byte(`"code":"InternalError"`)) || bytes.Contains(response.Body.Bytes(), []byte("private-path-and-secret")) {
		t.Fatalf("unsafe error response: %d %s", response.Code, response.Body.String())
	}
}

func TestFileUpdateRequiresMatchingETag(t *testing.T) {
	files := &fakeFileAPI{content: portyfs.FileContent{Path: "docker-compose.yml", Content: []byte("services: {}\n"), Hash: "sha256:old", Size: 13}}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{Files: files})

	request := httptest.NewRequest(stdhttp.MethodPut, "http://porty.local/api/v1/stacks/stk_gateway/files?path=docker-compose.yml", bytes.NewBufferString(`{"content":"services:\n  web: {}\n"}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusPreconditionRequired {
		t.Fatalf("missing If-Match status = %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(stdhttp.MethodPut, "http://porty.local/api/v1/stacks/stk_gateway/files?path=docker-compose.yml", bytes.NewBufferString(`{"content":"services:\n  web: {}\n"}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	request.Header.Set("If-Match", `"sha256:old"`)
	request.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusOK || files.expectedHash != "sha256:old" {
		t.Fatalf("save response = %d %s, hash=%q", response.Code, response.Body.String(), files.expectedHash)
	}
}

func TestLongRunningActionReturnsAcceptedOperationResource(t *testing.T) {
	actions := &fakeActionAPI{}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{Actions: actions})
	request := httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/stacks/stk_gateway/actions/deploy", nil)
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusAccepted || !bytes.Contains(response.Body.Bytes(), []byte(`"id":"op_1"`)) {
		t.Fatalf("action response = %d %s", response.Code, response.Body.String())
	}
}

func TestRepositorySetupAuditStateAndPaginationContracts(t *testing.T) {
	setup := &fakeRepositorySetup{}
	audit := &fakeAuditAPI{events: []portycontrol.AuditEvent{{ID: "aud_1", Action: "stack.delete"}}}
	state := &fakeStateAPI{}
	operations := &fakePagedOperations{}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup, Audit: audit, State: state, Operations: operations})

	request := httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/repository/setup", bytes.NewBufferString(`{"mode":"init","branch":"main"}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusCreated || setup.request.Mode != "init" {
		t.Fatalf("setup response = %d %s request=%#v", response.Code, response.Body.String(), setup.request)
	}

	for path, want := range map[string]string{
		"/api/v1/audit?limit=20&offset=40":      "aud_1",
		"/api/v1/stacks/stk_gateway/state":      "current",
		"/api/v1/operations?limit=20&offset=40": "op_page",
	} {
		req := httptest.NewRequest(stdhttp.MethodGet, "http://porty.local"+path, nil)
		req.AddCookie(sessionCookie)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != stdhttp.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(want)) {
			t.Fatalf("GET %s = %d %s", path, res.Code, res.Body.String())
		}
	}
	if operations.offset != 40 || audit.offset != 40 {
		t.Fatalf("pagination offsets: operations=%d audit=%d", operations.offset, audit.offset)
	}
}

func authenticatedAPIRouter(t *testing.T, extra RouterOptions) (stdhttp.Handler, *stdhttp.Cookie, string) {
	t.Helper()
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	extra.Readiness = func(context.Context) error { return nil }
	extra.Auth = auth
	extra.PublicURL = "http://porty.local"
	handler := NewRouter(extra)
	setupCSRF, setupCookie := setupToken(t, handler)
	register := httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/setup/register", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`))
	register.Header.Set("Origin", "http://porty.local")
	register.Header.Set("X-CSRF-Token", setupCSRF)
	register.AddCookie(setupCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, register)
	if response.Code != stdhttp.StatusCreated {
		t.Fatalf("register = %d: %s", response.Code, response.Body.String())
	}
	var session SessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	return handler, response.Result().Cookies()[0], session.CSRFToken
}

type fakeStackAPI struct {
	items   []portystack.Stack
	created string
	err     error
}

func (f *fakeStackAPI) ListStacks(context.Context) ([]portystack.Stack, error) { return f.items, f.err }
func (f *fakeStackAPI) CreateStack(_ context.Context, name string) (portystack.Stack, error) {
	f.created = name
	return portystack.Stack{ID: "stk_worker", DirectoryName: name}, nil
}
func (f *fakeStackAPI) RenameStack(context.Context, portystack.StackID, string) (portystack.Stack, error) {
	return portystack.Stack{}, nil
}
func (f *fakeStackAPI) DeleteStack(context.Context, portystack.StackID) error { return nil }
func (f *fakeStackAPI) PurgeStack(context.Context, portystack.StackID) error  { return nil }

type fakeFileAPI struct {
	content      portyfs.FileContent
	expectedHash string
}

type fakeActionAPI struct{}

func (*fakeActionAPI) StartAction(context.Context, portystack.StackID, string) (portyop.Operation, error) {
	return portyop.Operation{ID: "op_1", Status: portyop.OperationQueued}, nil
}

type fakeRepositorySetup struct {
	request       portyrepo.RepositorySetupRequest
	remoteRequest portyrepo.RepositoryRemoteRequest
	inspection    portyrepo.RemoteInspection
	status        portyrepo.RepositorySetupStatus
	err           error
	ready         bool
	readySet      bool
}

func (f *fakeRepositorySetup) Status(context.Context) (portyrepo.RepositorySetupStatus, error) {
	return f.status, f.err
}
func (f *fakeRepositorySetup) InspectRemote(context.Context, portyrepo.RemoteInspectionRequest) (portyrepo.RemoteInspection, error) {
	return f.inspection, f.err
}
func (f *fakeRepositorySetup) Setup(_ context.Context, request portyrepo.RepositorySetupRequest) (portyrepo.RepositorySetupStatus, error) {
	f.request = request
	return f.status, f.err
}
func (f *fakeRepositorySetup) ConfigureRemote(_ context.Context, request portyrepo.RepositoryRemoteRequest) (portyrepo.RepositorySetupStatus, error) {
	f.remoteRequest = request
	return f.status, f.err
}
func (f *fakeRepositorySetup) RemoveRemote(context.Context) (portyrepo.RepositorySetupStatus, error) {
	return f.status, f.err
}
func (f *fakeRepositorySetup) Ready(context.Context) (bool, error) {
	if !f.readySet {
		return true, nil
	}
	return f.ready, f.err
}

type fakeAuditAPI struct {
	events   []portycontrol.AuditEvent
	offset   int
	recorded []portycontrol.AuditEvent
}

func (f *fakeAuditAPI) AuditEvents(_ context.Context, _ int, offset int) ([]portycontrol.AuditEvent, error) {
	f.offset = offset
	return f.events, nil
}
func (f *fakeAuditAPI) RecordAudit(_ context.Context, event portycontrol.AuditEvent) error {
	f.recorded = append(f.recorded, event)
	return nil
}

type fakeStateAPI struct{}

func (*fakeStateAPI) StackState(context.Context, portystack.StackID) (portycontrol.StackState, error) {
	return portycontrol.StackState{Runtime: portycontrol.RuntimeRunning, Freshness: portycontrol.DeploymentCurrent}, nil
}

type fakePagedOperations struct{ offset int }

func (f *fakePagedOperations) Operation(context.Context, string) (portyop.Operation, error) {
	return portyop.Operation{}, nil
}
func (f *fakePagedOperations) Operations(context.Context, int) ([]portyop.Operation, error) {
	return nil, nil
}
func (f *fakePagedOperations) OperationsPage(_ context.Context, _, offset int) ([]portyop.Operation, error) {
	f.offset = offset
	return []portyop.Operation{{ID: "op_page"}}, nil
}
func (*fakeActionAPI) StartRepositoryAction(context.Context, string) (portyop.Operation, error) {
	return portyop.Operation{ID: "op_2", Status: portyop.OperationQueued}, nil
}

func (f *fakeFileAPI) Tree(context.Context, portystack.StackID) ([]portyfs.FileEntry, error) {
	return nil, nil
}
func (f *fakeFileAPI) ReadFile(context.Context, portystack.StackID, string) (portyfs.FileContent, error) {
	return f.content, nil
}
func (f *fakeFileAPI) WriteFile(_ context.Context, _ portystack.StackID, _ string, contents []byte, expected string) (portyfs.FileContent, error) {
	f.expectedHash = expected
	f.content.Content = contents
	f.content.Hash = "sha256:new"
	return f.content, nil
}
