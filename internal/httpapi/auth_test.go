package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	portyauth "github.com/msoldin/porty/internal/auth"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func TestOversizedJSONBodyIsRejected(t *testing.T) {
	handler := newAuthRouter(t)
	csrf, setupCookie := setupToken(t, handler)
	body := `{"username":"admin","password":"` + strings.Repeat("x", (1<<20)+1) + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/setup/register", strings.NewReader(body))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(setupCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d", response.Code)
	}
}

func TestTrailingJSONValueIsRejected(t *testing.T) {
	handler := newAuthRouter(t)
	csrf, setupCookie := setupToken(t, handler)
	request := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/setup/register", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}{}`))
	request.Header.Set("Origin", "http://porty.local")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(setupCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d", response.Code)
	}
}

func newAuthRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), func() time.Time { return now })
	return NewRouter(RouterOptions{
		Readiness:  func(context.Context) error { return nil },
		Auth:       service,
		PublicURL:  "http://porty.local",
		SecureHTTP: false,
	})
}

func setupToken(t *testing.T, handler http.Handler) (string, *http.Cookie) {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://porty.local/api/v1/setup/status", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("setup status = %d: %s", response.Code, response.Body.String())
	}
	var body SetupStatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Registered || body.CSRFToken == "" {
		t.Fatalf("setup status = %#v", body)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("setup cookies = %d, want 1", len(cookies))
	}
	return body.CSRFToken, cookies[0]
}

func TestRegisterLoginAndCSRFProtectedPasswordChange(t *testing.T) {
	handler := newAuthRouter(t)
	csrf, setupCookie := setupToken(t, handler)

	register := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/setup/register", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`))
	register.Header.Set("Content-Type", "application/json")
	register.Header.Set("Origin", "http://porty.local")
	register.Header.Set("X-CSRF-Token", csrf)
	register.AddCookie(setupCookie)
	registered := httptest.NewRecorder()
	handler.ServeHTTP(registered, register)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register status = %d: %s", registered.Code, registered.Body.String())
	}
	sessionCookie := registered.Result().Cookies()[0]
	var session SessionResponse
	if err := json.Unmarshal(registered.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	missingCSRF := httptest.NewRequest(http.MethodPut, "http://porty.local/api/v1/session/password", bytes.NewBufferString(`{"currentPassword":"correct horse battery staple","newPassword":"another correct horse battery staple"}`))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set("Origin", "http://porty.local")
	missingCSRF.AddCookie(sessionCookie)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, missingCSRF)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", denied.Code)
	}

	change := missingCSRF.Clone(context.Background())
	change.Body = http.NoBody
	change = httptest.NewRequest(http.MethodPut, "http://porty.local/api/v1/session/password", bytes.NewBufferString(`{"currentPassword":"correct horse battery staple","newPassword":"another correct horse battery staple"}`))
	change.Header.Set("Content-Type", "application/json")
	change.Header.Set("Origin", "http://porty.local")
	change.Header.Set("X-CSRF-Token", session.CSRFToken)
	change.AddCookie(sessionCookie)
	changed := httptest.NewRecorder()
	handler.ServeHTTP(changed, change)
	if changed.Code != http.StatusNoContent {
		t.Fatalf("password change status = %d: %s", changed.Code, changed.Body.String())
	}
}

func TestSecondRegistrationIsClosed(t *testing.T) {
	handler := newAuthRouter(t)
	csrf, setupCookie := setupToken(t, handler)
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/setup/register", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://porty.local")
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(setupCookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if attempt == 0 && response.Code != http.StatusCreated {
			t.Fatalf("first registration = %d", response.Code)
		}
		if attempt == 1 && response.Code != http.StatusConflict {
			t.Fatalf("second registration = %d, want 409", response.Code)
		}
	}
}

func TestSessionRefreshReturnsTokenFromBoundCSRFCookie(t *testing.T) {
	handler := newAuthRouter(t)
	csrf, setupCookie := setupToken(t, handler)
	register := httptest.NewRequest(http.MethodPost, "http://porty.local/api/v1/setup/register", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`))
	register.Header.Set("Content-Type", "application/json")
	register.Header.Set("Origin", "http://porty.local")
	register.Header.Set("X-CSRF-Token", csrf)
	register.AddCookie(setupCookie)
	registered := httptest.NewRecorder()
	handler.ServeHTTP(registered, register)

	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range registered.Result().Cookies() {
		switch cookie.Name {
		case sessionCookieName:
			sessionCookie = cookie
		case csrfCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("registration cookies session=%v csrf=%v", sessionCookie, csrfCookie)
	}

	request := httptest.NewRequest(http.MethodGet, "http://porty.local/api/v1/session", nil)
	request.AddCookie(sessionCookie)
	request.AddCookie(csrfCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body SessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.CSRFToken != csrfCookie.Value || body.CSRFToken == "" {
		t.Fatalf("session CSRF token = %q, want bound cookie value", body.CSRFToken)
	}
}

func TestPasswordChangeRemainsAvailableBeforeRepositorySetup(t *testing.T) {
	handler, session, csrf := authenticatedAPIRouter(t, RouterOptions{RepositorySetup: notReadyRepositorySetup()})
	response := doAuthenticatedRequest(handler, session, csrf, http.MethodPut, "/api/v1/session/password", `{"CurrentPassword":"correct horse battery staple","NewPassword":"new correct horse battery staple"}`)
	if response.Code != http.StatusNoContent {
		t.Fatalf("password change = %d %s", response.Code, response.Body.String())
	}
}
