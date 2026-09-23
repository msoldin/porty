package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
)

const (
	sessionCookieName = "porty_session"
	csrfCookieName    = "porty_csrf"
	setupCookieName   = "porty_setup"
)

type RouterOptions struct {
	Readiness       func(context.Context) error
	Auth            *application.AuthService
	PublicURL       string
	SecureHTTP      bool
	Stacks          StackAPI
	Files           FileAPI
	Environment     EnvironmentAPI
	Repository      RepositoryAPI
	Actions         ActionAPI
	Operations      OperationQueryAPI
	Deployments     DeploymentQueryAPI
	RepositorySetup RepositorySetupAPI
	Audit           AuditAPI
	State           StackStateAPI
	Stream          http.Handler
}

type StackAPI interface {
	ListStacks(context.Context) ([]domain.Stack, error)
	CreateStack(context.Context, string) (domain.Stack, error)
	RenameStack(context.Context, domain.StackID, string) (domain.Stack, error)
	DeleteStack(context.Context, domain.StackID) error
	PurgeStack(context.Context, domain.StackID) error
}

type FileAPI interface {
	Tree(context.Context, domain.StackID) ([]domain.FileEntry, error)
	ReadFile(context.Context, domain.StackID, string) (domain.FileContent, error)
	WriteFile(context.Context, domain.StackID, string, []byte, string) (domain.FileContent, error)
}

type FileMutationAPI interface {
	CreateFile(context.Context, domain.StackID, string, []byte) (domain.FileContent, error)
	CreateDirectory(context.Context, domain.StackID, string) error
	MoveFile(context.Context, domain.StackID, string, string) error
	RemoveFile(context.Context, domain.StackID, string) error
}

type EnvironmentAPI interface {
	EnvironmentKeys(context.Context, domain.StackID) ([]string, error)
	SetEnvironment(context.Context, domain.StackID, string, string) error
	DeleteEnvironment(context.Context, domain.StackID, string) error
}

type RepositoryAPI interface {
	RepositoryStatus(context.Context) (portyrepo.GitStatus, error)
	RepositoryHistory(context.Context, int) ([]portyrepo.GitCommit, error)
	StackDiff(context.Context, domain.StackID) (string, error)
	CommitStack(context.Context, domain.StackID, string) (string, error)
}

type ActionAPI interface {
	StartAction(context.Context, domain.StackID, string) (domain.Operation, error)
	StartRepositoryAction(context.Context, string) (domain.Operation, error)
}

type OperationQueryAPI interface {
	Operation(context.Context, string) (domain.Operation, error)
	Operations(context.Context, int) ([]domain.Operation, error)
}

type DeploymentQueryAPI interface {
	Deployments(context.Context, domain.StackID, int) ([]domain.Deployment, error)
}

type RepositorySetupAPI interface {
	Status(context.Context) (portyrepo.RepositorySetupStatus, error)
	InspectRemote(context.Context, portyrepo.RemoteInspectionRequest) (portyrepo.RemoteInspection, error)
	Setup(context.Context, portyrepo.RepositorySetupRequest) (portyrepo.RepositorySetupStatus, error)
	ConfigureRemote(context.Context, portyrepo.RepositoryRemoteRequest) (portyrepo.RepositorySetupStatus, error)
	RemoveRemote(context.Context) (portyrepo.RepositorySetupStatus, error)
	Ready(context.Context) (bool, error)
}

type AuditAPI interface {
	AuditEvents(context.Context, int, int) ([]domain.AuditEvent, error)
	RecordAudit(context.Context, domain.AuditEvent) error
}

type StackStateAPI interface {
	StackState(context.Context, domain.StackID) (domain.StackState, error)
}

type SetupStatusResponse struct {
	Registered bool   `json:"registered"`
	CSRFToken  string `json:"csrfToken,omitempty"`
}

type SessionResponse struct {
	Username  string `json:"username"`
	CSRFToken string `json:"csrfToken"`
}

func registerAuthRoutes(mux *http.ServeMux, options RouterOptions) {
	mux.HandleFunc("GET /api/v1/setup/status", func(w http.ResponseWriter, r *http.Request) {
		registered, err := options.Auth.Registered(r.Context())
		if err != nil {
			WriteError(w, r, http.StatusInternalServerError, "InternalError", "Unable to read setup state", nil)
			return
		}
		token := browserToken()
		setCookie(w, setupCookieName, token, "/api/v1/setup", options.SecureHTTP, 600)
		writeJSON(w, http.StatusOK, SetupStatusResponse{Registered: registered, CSRFToken: token})
	})

	mux.HandleFunc("POST /api/v1/setup/register", func(w http.ResponseWriter, r *http.Request) {
		if !validOrigin(r, options.PublicURL) || !validDoubleSubmit(r, setupCookieName) {
			WriteError(w, r, http.StatusForbidden, "PermissionDenied", "Request origin or CSRF token is invalid", nil)
			return
		}
		var input struct{ Username, Password string }
		if err := decodeJSON(r, &input); err != nil {
			WriteError(w, r, http.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
			return
		}
		credentials, err := options.Auth.Register(r.Context(), input.Username, input.Password)
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		setCookie(w, sessionCookieName, credentials.SessionToken, "/", options.SecureHTTP, 7*24*60*60)
		setCSRFCookie(w, credentials.CSRFToken, options.SecureHTTP, 7*24*60*60)
		recordAudit(options, r, "", "auth.register", "user", strings.TrimSpace(input.Username), "succeeded")
		writeJSON(w, http.StatusCreated, SessionResponse{Username: strings.TrimSpace(input.Username), CSRFToken: credentials.CSRFToken})
	})

	mux.HandleFunc("POST /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		if !validOrigin(r, options.PublicURL) {
			WriteError(w, r, http.StatusForbidden, "PermissionDenied", "Request origin is invalid", nil)
			return
		}
		var input struct{ Username, Password string }
		if err := decodeJSON(r, &input); err != nil {
			WriteError(w, r, http.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
			return
		}
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		credentials, err := options.Auth.Login(r.Context(), input.Username, input.Password, host)
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		setCookie(w, sessionCookieName, credentials.SessionToken, "/", options.SecureHTTP, 7*24*60*60)
		setCSRFCookie(w, credentials.CSRFToken, options.SecureHTTP, 7*24*60*60)
		recordAudit(options, r, "", "auth.login", "user", input.Username, "succeeded")
		writeJSON(w, http.StatusOK, SessionResponse{Username: input.Username, CSRFToken: credentials.CSRFToken})
	})

	mux.HandleFunc("GET /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		_, session, ok := authenticate(w, r, options.Auth)
		if !ok {
			return
		}
		csrf := ""
		if cookie, err := r.Cookie(csrfCookieName); err == nil && options.Auth.CheckCSRF(session, cookie.Value) {
			csrf = cookie.Value
		}
		writeJSON(w, http.StatusOK, SessionResponse{Username: session.Username, CSRFToken: csrf})
	})

	mux.HandleFunc("DELETE /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		raw, session, ok := requireMutationAuth(w, r, options)
		if !ok {
			return
		}
		_ = options.Auth.Logout(r.Context(), raw)
		recordAudit(options, r, session.UserID, "auth.logout", "user", session.UserID, "succeeded")
		setCookie(w, sessionCookieName, "", "/", options.SecureHTTP, -1)
		setCSRFCookie(w, "", options.SecureHTTP, -1)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PUT /api/v1/session/password", func(w http.ResponseWriter, r *http.Request) {
		raw, session, ok := requireMutationAuth(w, r, options)
		if !ok {
			return
		}
		var input struct{ CurrentPassword, NewPassword string }
		if err := decodeJSON(r, &input); err != nil {
			WriteError(w, r, http.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
			return
		}
		if err := options.Auth.ChangePassword(r.Context(), raw, input.CurrentPassword, input.NewPassword); err != nil {
			recordAudit(options, r, session.UserID, "auth.password.change", "user", session.UserID, "failed")
			writeAuthError(w, r, err)
			return
		}
		recordAudit(options, r, session.UserID, "auth.password.change", "user", session.UserID, "succeeded")
		setCookie(w, sessionCookieName, "", "/", options.SecureHTTP, -1)
		setCSRFCookie(w, "", options.SecureHTTP, -1)
		w.WriteHeader(http.StatusNoContent)
	})
}

func authenticate(w http.ResponseWriter, r *http.Request, service *application.AuthService) (string, application.AuthenticatedSession, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		WriteError(w, r, http.StatusUnauthorized, "AuthenticationFailed", "Authentication required", nil)
		return "", application.AuthenticatedSession{}, false
	}
	session, err := service.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		WriteError(w, r, http.StatusUnauthorized, "AuthenticationFailed", "Authentication required", nil)
		return "", application.AuthenticatedSession{}, false
	}
	return cookie.Value, session, true
}

func requireMutationAuth(w http.ResponseWriter, r *http.Request, options RouterOptions) (string, application.AuthenticatedSession, bool) {
	if !validOrigin(r, options.PublicURL) {
		WriteError(w, r, http.StatusForbidden, "PermissionDenied", "Request origin is invalid", nil)
		return "", application.AuthenticatedSession{}, false
	}
	raw, session, ok := authenticate(w, r, options.Auth)
	if !ok {
		return "", application.AuthenticatedSession{}, false
	}
	if !options.Auth.CheckCSRF(session, r.Header.Get("X-CSRF-Token")) {
		WriteError(w, r, http.StatusForbidden, "PermissionDenied", "CSRF token is invalid", nil)
		return "", application.AuthenticatedSession{}, false
	}
	return raw, session, true
}

func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, application.ErrAlreadyRegistered):
		WriteError(w, r, http.StatusConflict, "AlreadyRegistered", "Registration is closed", nil)
	case errors.Is(err, application.ErrRateLimited):
		WriteError(w, r, http.StatusTooManyRequests, "RateLimited", "Too many authentication attempts", nil)
	case errors.Is(err, application.ErrInvalidPassword), errors.Is(err, application.ErrInvalidUsername):
		WriteError(w, r, http.StatusBadRequest, "InvalidCredentials", err.Error(), nil)
	case errors.Is(err, application.ErrAuthenticationFailed):
		WriteError(w, r, http.StatusUnauthorized, "AuthenticationFailed", "Authentication failed", nil)
	default:
		WriteError(w, r, http.StatusInternalServerError, "InternalError", "Authentication could not be completed", nil)
	}
}

func decodeJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func validOrigin(r *http.Request, publicURL string) bool {
	expected := strings.TrimRight(publicURL, "/")
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	return r.Header.Get("Origin") == expected
}

func validDoubleSubmit(r *http.Request, name string) bool {
	cookie, err := r.Cookie(name)
	if err != nil {
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	return len(cookie.Value) == len(header) && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}

func browserToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func setCookie(w http.ResponseWriter, name, value, path string, secure bool, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func setCSRFCookie(w http.ResponseWriter, value string, secure bool, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: value, Path: "/", HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}
