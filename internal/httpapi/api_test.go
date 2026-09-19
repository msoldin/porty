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
