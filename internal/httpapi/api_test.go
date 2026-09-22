package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
	portyauth "github.com/msoldin/porty/internal/infrastructure/auth"
	portysqlite "github.com/msoldin/porty/internal/infrastructure/sqlite"
)

func TestStackEndpointsRequireSessionAndMutationsRequireCSRF(t *testing.T) {
	stacks := &fakeStackAPI{items: []domain.Stack{{ID: "stk_gateway", DirectoryName: "gateway"}}}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{Stacks: stacks})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://porty.local/api/v1/stacks", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "http://porty.local/api/v1/stacks", nil)
	listRequest.AddCookie(sessionCookie)
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte("gateway")) {
		t.Fatalf("list response = %d %s", listed.Code, listed.Body.String())
	}

	create := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/stacks", bytes.NewBufferString(`{"name":"worker"}`))
	create.Header.Set("Origin", "http://porty.local")
	create.AddCookie(sessionCookie)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, create)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", denied.Code)
	}

	create = httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/stacks", bytes.NewBufferString(`{"name":"worker"}`))
	create.Header.Set("Origin", "http://porty.local")
	create.Header.Set("X-CSRF-Token", csrf)
	create.AddCookie(sessionCookie)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || stacks.created != "worker" {
		t.Fatalf("create response = %d %s", created.Code, created.Body.String())
	}
}

func TestFileUpdateRequiresMatchingETag(t *testing.T) {
	files := &fakeFileAPI{content: domain.FileContent{Path: "docker-compose.yml", Content: []byte("services: {}\n"), Hash: "sha256:old", Size: 13}}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{Files: files})

	request := httptest.NewRequest(http.MethodPut, "http://porty.local/api/v1/stacks/stk_gateway/files?path=docker-compose.yml", bytes.NewBufferString(`{"content":"services:\n  web: {}\n"}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing If-Match status = %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "http://porty.local/api/v1/stacks/stk_gateway/files?path=docker-compose.yml", bytes.NewBufferString(`{"content":"services:\n  web: {}\n"}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("If-Match", `"sha256:old"`)
	request.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || files.expectedHash != "sha256:old" {
		t.Fatalf("save response = %d %s, hash=%q", response.Code, response.Body.String(), files.expectedHash)
	}
}

func TestLongRunningActionReturnsAcceptedOperationResource(t *testing.T) {
	actions := &fakeActionAPI{}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{Actions: actions})
	request := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/stacks/stk_gateway/actions/deploy", nil)
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !bytes.Contains(response.Body.Bytes(), []byte(`"id":"op_1"`)) {
		t.Fatalf("action response = %d %s", response.Code, response.Body.String())
	}
}

func TestRepositorySetupAuditStateAndPaginationContracts(t *testing.T) {
	setup := &fakeRepositorySetup{}
	audit := &fakeAuditAPI{events: []domain.AuditEvent{{ID: "aud_1", Action: "stack.delete"}}}
	state := &fakeStateAPI{}
	operations := &fakePagedOperations{}
	handler, sessionCookie, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: setup, Audit: audit, State: state, Operations: operations})

	request := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/repository/setup", bytes.NewBufferString(`{"mode":"init","branch":"main"}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || setup.request.Mode != "init" {
		t.Fatalf("setup response = %d %s request=%#v", response.Code, response.Body.String(), setup.request)
	}

	for path, want := range map[string]string{
		"/api/v1/audit?limit=20&offset=40":      "aud_1",
		"/api/v1/stacks/stk_gateway/state":      "current",
		"/api/v1/operations?limit=20&offset=40": "op_page",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://porty.local"+path, nil)
		req.AddCookie(sessionCookie)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(want)) {
			t.Fatalf("GET %s = %d %s", path, res.Code, res.Body.String())
		}
	}
	if operations.offset != 40 || audit.offset != 40 {
		t.Fatalf("pagination offsets: operations=%d audit=%d", operations.offset, audit.offset)
	}
}

func authenticatedAPIRouter(t *testing.T, extra RouterOptions) (http.Handler, *http.Cookie, string) {
	t.Helper()
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := application.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	extra.Readiness = func(context.Context) error { return nil }
	extra.Auth = auth
	extra.PublicURL = "http://porty.local"
	handler := NewRouter(extra)
	setupCSRF, setupCookie := setupToken(t, handler)
	register := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/setup/register", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`))
	register.Header.Set("Origin", "http://porty.local")
	register.Header.Set("X-CSRF-Token", setupCSRF)
	register.AddCookie(setupCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, register)
	if response.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", response.Code, response.Body.String())
	}
	var session SessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	return handler, response.Result().Cookies()[0], session.CSRFToken
}

type fakeStackAPI struct {
	items   []domain.Stack
	created string
}

func (f *fakeStackAPI) ListStacks(context.Context) ([]domain.Stack, error) { return f.items, nil }
func (f *fakeStackAPI) CreateStack(_ context.Context, name string) (domain.Stack, error) {
	f.created = name
	return domain.Stack{ID: "stk_worker", DirectoryName: name}, nil
}
func (f *fakeStackAPI) RenameStack(context.Context, domain.StackID, string) (domain.Stack, error) {
	return domain.Stack{}, nil
}
func (f *fakeStackAPI) DeleteStack(context.Context, domain.StackID) error { return nil }
func (f *fakeStackAPI) PurgeStack(context.Context, domain.StackID) error  { return nil }

type fakeFileAPI struct {
	content      domain.FileContent
	expectedHash string
}

type fakeActionAPI struct{}

func (*fakeActionAPI) StartAction(context.Context, domain.StackID, string) (domain.Operation, error) {
	return domain.Operation{ID: "op_1", Status: domain.OperationQueued}, nil
}

type fakeRepositorySetup struct {
	request       domain.RepositorySetupRequest
	remoteRequest domain.RepositoryRemoteRequest
	inspection    domain.RemoteInspection
	status        domain.RepositorySetupStatus
	err           error
	ready         bool
	readySet      bool
}

func (f *fakeRepositorySetup) Status(context.Context) (domain.RepositorySetupStatus, error) {
	return f.status, f.err
}
func (f *fakeRepositorySetup) InspectRemote(context.Context, domain.RemoteInspectionRequest) (domain.RemoteInspection, error) {
	return f.inspection, f.err
}
func (f *fakeRepositorySetup) Setup(_ context.Context, request domain.RepositorySetupRequest) (domain.RepositorySetupStatus, error) {
	f.request = request
	return f.status, f.err
}
func (f *fakeRepositorySetup) ConfigureRemote(_ context.Context, request domain.RepositoryRemoteRequest) (domain.RepositorySetupStatus, error) {
	f.remoteRequest = request
	return f.status, f.err
}
func (f *fakeRepositorySetup) RemoveRemote(context.Context) (domain.RepositorySetupStatus, error) {
	return f.status, f.err
}
func (f *fakeRepositorySetup) Ready(context.Context) (bool, error) {
	if !f.readySet {
		return true, nil
	}
	return f.ready, f.err
}

type fakeAuditAPI struct {
	events   []domain.AuditEvent
	offset   int
	recorded []domain.AuditEvent
}

func (f *fakeAuditAPI) AuditEvents(_ context.Context, _ int, offset int) ([]domain.AuditEvent, error) {
	f.offset = offset
	return f.events, nil
}
func (f *fakeAuditAPI) RecordAudit(_ context.Context, event domain.AuditEvent) error {
	f.recorded = append(f.recorded, event)
	return nil
}

type fakeStateAPI struct{}

func (*fakeStateAPI) StackState(context.Context, domain.StackID) (domain.StackState, error) {
	return domain.StackState{Runtime: domain.RuntimeRunning, Freshness: domain.DeploymentCurrent}, nil
}

type fakePagedOperations struct{ offset int }

func (f *fakePagedOperations) Operation(context.Context, string) (domain.Operation, error) {
	return domain.Operation{}, nil
}
func (f *fakePagedOperations) Operations(context.Context, int) ([]domain.Operation, error) {
	return nil, nil
}
func (f *fakePagedOperations) OperationsPage(_ context.Context, _, offset int) ([]domain.Operation, error) {
	f.offset = offset
	return []domain.Operation{{ID: "op_page"}}, nil
}
func (*fakeActionAPI) StartRepositoryAction(context.Context, string) (domain.Operation, error) {
	return domain.Operation{ID: "op_2", Status: domain.OperationQueued}, nil
}

func (f *fakeFileAPI) Tree(context.Context, domain.StackID) ([]domain.FileEntry, error) {
	return nil, nil
}
func (f *fakeFileAPI) ReadFile(context.Context, domain.StackID, string) (domain.FileContent, error) {
	return f.content, nil
}
func (f *fakeFileAPI) WriteFile(_ context.Context, _ domain.StackID, _ string, contents []byte, expected string) (domain.FileContent, error) {
	f.expectedHash = expected
	f.content.Content = contents
	f.content.Hash = "sha256:new"
	return f.content, nil
}
