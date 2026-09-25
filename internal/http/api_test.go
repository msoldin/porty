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
	portycompose "github.com/msoldin/porty/internal/compose"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyfs "github.com/msoldin/porty/internal/filesystem"
	portyop "github.com/msoldin/porty/internal/operation"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
	portystack "github.com/msoldin/porty/internal/stack"
)

func TestEnvironmentReadRequiresSessionAndReturnsOnlyRequestedValue(t *testing.T) {
	audit := &fakeAuditAPI{}
	environment := &fakeEnvironmentAPI{values: map[string]string{"TOKEN": "secret", "EMPTY": ""}, secrets: map[string]bool{"TOKEN": true}}
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
		{base + "/TOKEN", 200, `{"value":"secret","secret":true}` + "\n"},
		{base + "/EMPTY", 200, `{"value":"","secret":false}` + "\n"},
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

func TestEnvironmentWritePersistsExplicitSecretFlag(t *testing.T) {
	environment := &fakeEnvironmentAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Environment: environment})
	request := httptest.NewRequest(stdhttp.MethodPut, "http://porty.local/api/v1/stacks/stk_gateway/environment/TOKEN", bytes.NewBufferString(`{"value":"secret","secret":true}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusNoContent || environment.savedSecret == nil || !*environment.savedSecret {
		t.Fatalf("write response = %d, secret = %v", response.Code, environment.savedSecret)
	}
}

func TestEnvironmentWriteRejectsSecretSettingChange(t *testing.T) {
	environment := &fakeEnvironmentAPI{setErr: portystack.ErrEnvironmentSecretImmutable}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Environment: environment})
	request := httptest.NewRequest(stdhttp.MethodPut, "http://porty.local/api/v1/stacks/stk_gateway/environment/TOKEN", bytes.NewBufferString(`{"value":"replacement","secret":false}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, stdhttp.StatusConflict, "EnvironmentSecretImmutable")
}

type fakeEnvironmentAPI struct {
	values      map[string]string
	secrets     map[string]bool
	savedSecret *bool
	setErr      error
}

func (f *fakeEnvironmentAPI) EnvironmentKeys(_ context.Context, id portystack.StackID) ([]string, error) {
	if id != "stk_gateway" {
		return nil, sql.ErrNoRows
	}
	return []string{"EMPTY", "TOKEN"}, nil
}
func (f *fakeEnvironmentAPI) EnvironmentValue(_ context.Context, id portystack.StackID, key string) (portystack.EnvironmentValue, error) {
	if strings.Contains(key, "-") {
		return portystack.EnvironmentValue{}, portystack.ErrInvalidEnvironment
	}
	if id != "stk_gateway" {
		return portystack.EnvironmentValue{}, sql.ErrNoRows
	}
	value, ok := f.values[key]
	if !ok {
		return portystack.EnvironmentValue{}, sql.ErrNoRows
	}
	return portystack.EnvironmentValue{Value: value, Secret: f.secrets[key]}, nil
}
func (f *fakeEnvironmentAPI) SetEnvironmentWithSecret(_ context.Context, _ portystack.StackID, _, _ string, secret bool) error {
	f.savedSecret = &secret
	return f.setErr
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

func TestStackRuntimeActionConflictUsesSafeHTTPStatus(t *testing.T) {
	actions := &fakeActionAPI{err: portycontrol.ErrStackRuntimeActionUnavailable}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Actions: actions})
	request := httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/stacks/stk_gateway/actions/stop", nil)
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, stdhttp.StatusConflict, "StackRuntimeActionUnavailable")
}

func TestContainerListRequiresSessionAndReturnsMetadataRows(t *testing.T) {
	containers := &fakeContainerAPI{items: []portycontrol.Container{
		{ID: "id-a", Name: "app-1", Service: "app", State: "running", Health: "healthy"},
		{ID: "id-b", Name: "app-2", Service: "app", State: "exited"},
	}}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Containers: containers})
	path := "http://porty.local/api/v1/stacks/stk_gateway/containers"
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(stdhttp.MethodGet, path, nil))
	if unauthorized.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d", unauthorized.Code)
	}
	request := httptest.NewRequest(stdhttp.MethodGet, path, nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("container list = %d: %s", response.Code, response.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(rows[0]) != 8 || rows[0]["id"] != "id-a" || rows[1]["state"] != "exited" {
		t.Fatalf("container list = %#v", rows)
	}
}

func TestContainerMutationRequiresOriginAndCSRF(t *testing.T) {
	containers := &fakeContainerAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Containers: containers})
	path := "http://porty.local/api/v1/stacks/stk_gateway/containers/full-id-b/actions/stop"
	for _, tc := range []struct {
		name, origin          string
		withSession, withCSRF bool
		want                  int
	}{
		{name: "no session", origin: "http://porty.local", withCSRF: true, want: stdhttp.StatusUnauthorized},
		{name: "foreign origin", origin: "http://evil.local", withSession: true, withCSRF: true, want: stdhttp.StatusForbidden},
		{name: "missing csrf", origin: "http://porty.local", withSession: true, want: stdhttp.StatusForbidden},
		{name: "valid", origin: "http://porty.local", withSession: true, withCSRF: true, want: stdhttp.StatusAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			containers.calledID, containers.calledAction = "", ""
			request := httptest.NewRequest(stdhttp.MethodPost, path, nil)
			request.Header.Set("Origin", tc.origin)
			if tc.withSession {
				request.AddCookie(session)
			}
			if tc.withCSRF {
				request.Header.Set("X-CSRF-Token", csrf)
				request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("action response = %d: %s", response.Code, response.Body.String())
			}
			if tc.want == stdhttp.StatusAccepted {
				if containers.calledID != "full-id-b" || containers.calledAction != "stop" || !bytes.Contains(response.Body.Bytes(), []byte(`"id":"op_container"`)) {
					t.Fatalf("accepted action = %q %q %s", containers.calledID, containers.calledAction, response.Body.String())
				}
			} else if containers.calledID != "" {
				t.Fatalf("rejected action reached control: %q", containers.calledID)
			}
		})
	}
}

func TestContainerActionErrorsUseSafeHTTPStatuses(t *testing.T) {
	containers := &fakeContainerAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Containers: containers})
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"missing", portycontrol.ErrContainerNotFound, stdhttp.StatusNotFound, "ContainerNotFound"},
		{"state", portycontrol.ErrContainerStateConflict, stdhttp.StatusConflict, "ContainerStateConflict"},
		{"archived", portycontrol.ErrContainerArchived, stdhttp.StatusConflict, "ContainerStateConflict"},
		{"action", portycontrol.ErrUnsupportedContainerAction, stdhttp.StatusBadRequest, "InvalidContainerAction"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			containers.err = tc.err
			request := httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/stacks/stk_gateway/containers/full-id-b/actions/stop", nil)
			request.AddCookie(session)
			request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
			request.Header.Set("Origin", "http://porty.local")
			request.Header.Set("X-CSRF-Token", csrf)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertAPIError(t, response, tc.status, tc.code)
		})
	}
}

type fakeContainerAPI struct {
	items                  []portycontrol.Container
	calledID, calledAction string
	batchIDs               []string
	err                    error
	logSnapshot            portycompose.ContainerLogSnapshot
	inspectJSON            json.RawMessage
}

func (f *fakeContainerAPI) Containers(context.Context, portystack.StackID) ([]portycontrol.Container, error) {
	return f.items, f.err
}

func (f *fakeContainerAPI) StartContainerAction(_ context.Context, _ portystack.StackID, id, action string) (portyop.Operation, error) {
	f.calledID, f.calledAction = id, action
	return portyop.Operation{ID: "op_container", Kind: "container_" + action}, f.err
}

func (f *fakeContainerAPI) StartContainerBatchAction(_ context.Context, _ portystack.StackID, ids []string, action string) (portyop.Operation, error) {
	f.batchIDs, f.calledAction = ids, action
	return portyop.Operation{ID: "op_batch", Kind: "container_batch_" + action}, f.err
}

func (f *fakeContainerAPI) ContainerLogs(_ context.Context, _ portystack.StackID, id string) (portycompose.ContainerLogSnapshot, error) {
	f.calledID = id
	return f.logSnapshot, f.err
}

func (f *fakeContainerAPI) ContainerInspect(_ context.Context, _ portystack.StackID, id string) (json.RawMessage, error) {
	f.calledID = id
	return f.inspectJSON, f.err
}

func TestContainerDetailsRoutesRequireSessionAndNoStore(t *testing.T) {
	containers := &fakeContainerAPI{
		logSnapshot: portycompose.ContainerLogSnapshot{Output: "ready\n", Truncated: false},
		inspectJSON: json.RawMessage(`{"Id":"full-id-b","Config":{"Env":["TOKEN=secret"]},"Size":9007199254740993}`),
	}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Containers: containers})
	base := "http://porty.local/api/v1/stacks/stk_gateway/containers/full-id-b"
	for _, tc := range []struct{ suffix, body string }{
		{"/logs", `"output":"ready\n"`},
		{"/inspect", `"TOKEN=secret"`},
	} {
		unauthorized := httptest.NewRecorder()
		handler.ServeHTTP(unauthorized, httptest.NewRequest(stdhttp.MethodGet, base+tc.suffix, nil))
		if unauthorized.Code != stdhttp.StatusUnauthorized || unauthorized.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unauthorized %s = %d, cache=%q", tc.suffix, unauthorized.Code, unauthorized.Header().Get("Cache-Control"))
		}
		request := httptest.NewRequest(stdhttp.MethodGet, base+tc.suffix, nil)
		request.AddCookie(session)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != stdhttp.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), tc.body) {
			t.Fatalf("GET %s = %d cache=%q body=%s", tc.suffix, response.Code, response.Header().Get("Cache-Control"), response.Body.String())
		}
		if containers.calledID != "full-id-b" {
			t.Fatalf("targeted ID = %q", containers.calledID)
		}
	}
	request := httptest.NewRequest(stdhttp.MethodGet, base+"/inspect", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), "9007199254740993") {
		t.Fatalf("inspect lost integer precision: %s", response.Body.String())
	}
}

func TestContainerDetailsRoutesMapNotFoundAndLimit(t *testing.T) {
	containers := &fakeContainerAPI{}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{Containers: containers})
	base := "http://porty.local/api/v1/stacks/stk_gateway/containers/full-id-b"
	for _, tc := range []struct {
		suffix string
		err    error
		status int
		code   string
	}{
		{"/logs", portycontrol.ErrContainerNotFound, stdhttp.StatusNotFound, "ContainerNotFound"},
		{"/inspect", portycontrol.ErrContainerInspectTooLarge, stdhttp.StatusRequestEntityTooLarge, "LimitExceeded"},
	} {
		containers.err = tc.err
		request := httptest.NewRequest(stdhttp.MethodGet, base+tc.suffix, nil)
		request.AddCookie(session)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertAPIError(t, response, tc.status, tc.code)
		if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("unsafe error response: %s", response.Body.String())
		}
	}
}

func TestContainerBatchRouteRequiresSessionOriginAndCSRF(t *testing.T) {
	containers := &fakeContainerAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Containers: containers})
	path := "http://porty.local/api/v1/stacks/stk_gateway/containers/actions/stop"
	for _, tc := range []struct {
		name, origin  string
		session, csrf bool
		want          int
	}{
		{"no session", "http://porty.local", false, true, stdhttp.StatusUnauthorized},
		{"wrong origin", "http://evil.local", true, true, stdhttp.StatusForbidden},
		{"no csrf", "http://porty.local", true, false, stdhttp.StatusForbidden},
		{"valid", "http://porty.local", true, true, stdhttp.StatusAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			containers.batchIDs = nil
			request := httptest.NewRequest(stdhttp.MethodPost, path, bytes.NewBufferString(`{"containerIds":["full-id-b"]}`))
			request.Header.Set("Origin", tc.origin)
			if tc.session {
				request.AddCookie(session)
			}
			if tc.csrf {
				request.Header.Set("X-CSRF-Token", csrf)
				request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.want == stdhttp.StatusAccepted {
				if len(containers.batchIDs) != 1 || containers.batchIDs[0] != "full-id-b" || containers.calledAction != "stop" ||
					!bytes.Contains(response.Body.Bytes(), []byte(`"id":"op_batch"`)) {
					t.Fatalf("dispatch=%#v %s response=%s", containers.batchIDs, containers.calledAction, response.Body.String())
				}
			} else if containers.batchIDs != nil {
				t.Fatalf("unauthorized batch reached control: %#v", containers.batchIDs)
			}
		})
	}
}

func TestContainerBatchRouteValidatesBodyAndMapsErrors(t *testing.T) {
	containers := &fakeContainerAPI{}
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{Containers: containers})
	for _, tc := range []struct {
		name, body string
		err        error
		status     int
		code       string
	}{
		{"malformed", "{", nil, stdhttp.StatusBadRequest, "InvalidRequest"},
		{"selection", `{"containerIds":[]}`, portycontrol.ErrInvalidContainerSelection, stdhttp.StatusBadRequest, "InvalidContainerSelection"},
		{"foreign", `{"containerIds":["foreign"]}`, portycontrol.ErrContainerNotFound, stdhttp.StatusNotFound, "ContainerNotFound"},
		{"state", `{"containerIds":["full-id-b"]}`, portycontrol.ErrContainerStateConflict, stdhttp.StatusConflict, "ContainerStateConflict"},
		{"conflict", `{"containerIds":["full-id-b"]}`, portyop.ErrOperationConflict, stdhttp.StatusConflict, "OperationConflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			containers.err = tc.err
			containers.batchIDs = nil
			request := httptest.NewRequest(stdhttp.MethodPost, "http://porty.local/api/v1/stacks/stk_gateway/containers/actions/stop", bytes.NewBufferString(tc.body))
			request.Header.Set("Origin", "http://porty.local")
			request.Header.Set("X-CSRF-Token", csrf)
			request.AddCookie(&stdhttp.Cookie{Name: csrfCookieName, Value: csrf})
			request.AddCookie(session)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertAPIError(t, response, tc.status, tc.code)
			if tc.name == "malformed" && containers.batchIDs != nil {
				t.Fatal("malformed body reached control")
			}
		})
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

type fakeStackDeploymentTimes struct {
	values map[portystack.StackID]time.Time
	err    error
}

func (f *fakeStackDeploymentTimes) LatestDeploymentTimes(context.Context) (map[portystack.StackID]time.Time, error) {
	return f.values, f.err
}

func TestStackListIncludesLastDeploymentWithoutDocker(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 30, 0, 0, time.UTC)
	times := &fakeStackDeploymentTimes{values: map[portystack.StackID]time.Time{"active": at}}
	handler, session, _ := authenticatedAPIRouter(t, RouterOptions{
		Stacks: &fakeStackAPI{items: []portystack.Stack{
			{ID: "active", DirectoryName: "active"},
			{ID: "never", DirectoryName: "never"},
		}},
		StackDeploymentTimes: times,
	})
	request := httptest.NewRequest(stdhttp.MethodGet, "http://porty.local/api/v1/stacks", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("GET stacks = %d: %s", response.Code, response.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if got := rows[0]["lastDeploymentAt"]; got != at.Format(time.RFC3339) {
		t.Fatalf("active last deployment = %v", got)
	}
	if _, ok := rows[1]["lastDeploymentAt"]; ok {
		t.Fatal("never-deployed stack has a timestamp")
	}
	times.err = errors.New("database unavailable")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("deployment read error = %d: %s", response.Code, response.Body.String())
	}
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

type fakeActionAPI struct{ err error }

func (f *fakeActionAPI) StartAction(context.Context, portystack.StackID, string) (portyop.Operation, error) {
	return portyop.Operation{ID: "op_1", Status: portyop.OperationQueued}, f.err
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
