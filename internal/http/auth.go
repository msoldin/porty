package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	portyauth "github.com/msoldin/porty/internal/auth"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"strings"
	"time"

	portycontrol "github.com/msoldin/porty/internal/control"
	portyfs "github.com/msoldin/porty/internal/filesystem"
	portyop "github.com/msoldin/porty/internal/operation"
	portystack "github.com/msoldin/porty/internal/stack"
)

const (
	sessionCookieName = "porty_session"
	refreshCookieName = "porty_refresh"
	csrfCookieName    = "porty_csrf"
	setupCookieName   = "porty_setup"
)

type RouterOptions struct {
	AccessLogger         *slog.Logger
	Readiness            func(context.Context) error
	Auth                 *portyauth.AuthService
	PublicURL            string
	SecureHTTP           bool
	Stacks               StackAPI
	Files                FileAPI
	Environment          EnvironmentAPI
	Repository           RepositoryAPI
	Actions              ActionAPI
	Operations           OperationQueryAPI
	Deployments          DeploymentQueryAPI
	StackDeploymentTimes StackDeploymentTimesAPI
	RepositorySetup      RepositorySetupAPI
	Audit                AuditAPI
	State                StackStateAPI
	Containers           ContainerAPI
	Stream               stdhttp.Handler
}

type StackAPI interface {
	ListStacks(context.Context) ([]portystack.Stack, error)
	CreateStack(context.Context, string) (portystack.Stack, error)
	RenameStack(context.Context, portystack.StackID, string) (portystack.Stack, error)
	DeleteStack(context.Context, portystack.StackID) error
	PurgeStack(context.Context, portystack.StackID) error
}

type FileAPI interface {
	Tree(context.Context, portystack.StackID) ([]portyfs.FileEntry, error)
	ReadFile(context.Context, portystack.StackID, string) (portyfs.FileContent, error)
	WriteFile(context.Context, portystack.StackID, string, []byte, string) (portyfs.FileContent, error)
}

type FileMutationAPI interface {
	CreateFile(context.Context, portystack.StackID, string, []byte) (portyfs.FileContent, error)
	CreateDirectory(context.Context, portystack.StackID, string) error
	MoveFile(context.Context, portystack.StackID, string, string) error
	RemoveFile(context.Context, portystack.StackID, string) error
}

type EnvironmentAPI interface {
	EnvironmentKeys(context.Context, portystack.StackID) ([]string, error)
	EnvironmentValue(context.Context, portystack.StackID, string) (portystack.EnvironmentValue, error)
	SetEnvironment(context.Context, portystack.StackID, string, string) error
	SetEnvironmentWithSecret(context.Context, portystack.StackID, string, string, bool) error
	DeleteEnvironment(context.Context, portystack.StackID, string) error
}

type RepositoryAPI interface {
	RepositoryStatus(context.Context) (portyrepo.GitStatus, error)
	RepositoryHistory(context.Context, int) ([]portyrepo.GitCommit, error)
	StackDiff(context.Context, portystack.StackID) (string, error)
	CommitStack(context.Context, portystack.StackID, string) (string, error)
}

type ActionAPI interface {
	StartAction(context.Context, portystack.StackID, string) (portyop.Operation, error)
	StartRepositoryAction(context.Context, string) (portyop.Operation, error)
}

type OperationQueryAPI interface {
	Operation(context.Context, string) (portyop.Operation, error)
	Operations(context.Context, int) ([]portyop.Operation, error)
}

type DeploymentQueryAPI interface {
	Deployments(context.Context, portystack.StackID, int) ([]portyop.Deployment, error)
}

type StackDeploymentTimesAPI interface {
	LatestDeploymentTimes(context.Context) (map[portystack.StackID]time.Time, error)
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
	AuditEvents(context.Context, int, int) ([]portycontrol.AuditEvent, error)
	RecordAudit(context.Context, portycontrol.AuditEvent) error
}

type StackStateAPI interface {
	StackState(context.Context, portystack.StackID) (portycontrol.StackState, error)
}

type ContainerAPI interface {
	Containers(context.Context, portystack.StackID) ([]portycontrol.Container, error)
	StartContainerAction(context.Context, portystack.StackID, string, string) (portyop.Operation, error)
}

type SetupStatusResponse struct {
	Registered bool   `json:"registered"`
	CSRFToken  string `json:"csrfToken,omitempty"`
}

type SessionResponse struct {
	Username  string `json:"username"`
	CSRFToken string `json:"csrfToken"`
}

func registerAuthRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	mux.HandleFunc("GET /api/v1/setup/status", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		registered, err := options.Auth.Registered(r.Context())
		if err != nil {
			WriteError(w, r, stdhttp.StatusInternalServerError, "InternalError", "Unable to read setup state", nil)
			return
		}
		token := browserToken()
		setCookie(w, setupCookieName, token, "/api/v1/setup", authCookieSecure(options), 600)
		writeJSON(w, stdhttp.StatusOK, SetupStatusResponse{Registered: registered, CSRFToken: token})
	})

	mux.HandleFunc("POST /api/v1/setup/register", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !validOrigin(r, options.PublicURL) || !validDoubleSubmit(r, setupCookieName) {
			WriteError(w, r, stdhttp.StatusForbidden, "PermissionDenied", "Request origin or CSRF token is invalid", nil)
			return
		}
		var input struct{ Username, Password string }
		if err := decodeJSON(r, &input); err != nil {
			WriteError(w, r, stdhttp.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
			return
		}
		credentials, err := options.Auth.Register(r.Context(), input.Username, input.Password)
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		setAuthCookies(w, credentials, options)
		recordAudit(options, r, "", "auth.register", "user", strings.TrimSpace(input.Username), "succeeded")
		writeJSON(w, stdhttp.StatusCreated, SessionResponse{Username: strings.TrimSpace(input.Username), CSRFToken: credentials.CSRFToken})
	})

	mux.HandleFunc("POST /api/v1/session", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !validOrigin(r, options.PublicURL) {
			WriteError(w, r, stdhttp.StatusForbidden, "PermissionDenied", "Request origin is invalid", nil)
			return
		}
		var input struct{ Username, Password string }
		if err := decodeJSON(r, &input); err != nil {
			WriteError(w, r, stdhttp.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
			return
		}
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		credentials, err := options.Auth.Login(r.Context(), input.Username, input.Password, host)
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		setAuthCookies(w, credentials, options)
		recordAudit(options, r, "", "auth.login", "user", input.Username, "succeeded")
		writeJSON(w, stdhttp.StatusOK, SessionResponse{Username: input.Username, CSRFToken: credentials.CSRFToken})
	})

	mux.HandleFunc("POST /api/v1/session/refresh", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !validOrigin(r, options.PublicURL) || !validDoubleSubmit(r, csrfCookieName) {
			WriteError(w, r, stdhttp.StatusForbidden, "PermissionDenied", "Request origin or CSRF token is invalid", nil)
			return
		}
		cookie, err := r.Cookie(refreshCookieName)
		if err != nil {
			writeAuthError(w, r, portyauth.ErrAuthenticationFailed)
			return
		}
		credentials, err := options.Auth.Refresh(r.Context(), cookie.Value, r.Header.Get("X-CSRF-Token"))
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		setAuthCookies(w, credentials, options)
		writeJSON(w, stdhttp.StatusOK, SessionResponse{Username: credentials.Username, CSRFToken: credentials.CSRFToken})
	})

	mux.HandleFunc("GET /api/v1/session", authenticatedRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		session := principalFrom(r.Context())
		csrf := ""
		if cookie, err := r.Cookie(csrfCookieName); err == nil && options.Auth.CheckCSRF(session, cookie.Value) {
			csrf = cookie.Value
		}
		writeJSON(w, stdhttp.StatusOK, SessionResponse{Username: session.Username, CSRFToken: csrf})
	}))

	mux.HandleFunc("DELETE /api/v1/session", mutationAuthRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		session := principalFrom(r.Context())
		refreshCookie, err := r.Cookie(refreshCookieName)
		if err != nil || options.Auth.Logout(r.Context(), refreshCookie.Value, session.UserID) != nil {
			writeAuthError(w, r, portyauth.ErrAuthenticationFailed)
			return
		}
		recordAudit(options, r, session.UserID, "auth.logout", "user", session.UserID, "succeeded")
		clearAuthCookies(w, options)
		w.WriteHeader(stdhttp.StatusNoContent)
	}))

	mux.HandleFunc("PUT /api/v1/session/password", mutationAuthRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		session := principalFrom(r.Context())
		accessCookie, _ := r.Cookie(sessionCookieName)
		var input struct{ CurrentPassword, NewPassword string }
		if err := decodeJSON(r, &input); err != nil {
			WriteError(w, r, stdhttp.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
			return
		}
		if err := options.Auth.ChangePassword(r.Context(), accessCookie.Value, input.CurrentPassword, input.NewPassword); err != nil {
			recordAudit(options, r, session.UserID, "auth.password.change", "user", session.UserID, "failed")
			writeAuthError(w, r, err)
			return
		}
		recordAudit(options, r, session.UserID, "auth.password.change", "user", session.UserID, "succeeded")
		clearAuthCookies(w, options)
		w.WriteHeader(stdhttp.StatusNoContent)
	}))
}

func authCookieSecure(options RouterOptions) bool {
	return options.SecureHTTP || strings.HasPrefix(options.PublicURL, "https://")
}
func setAuthCookies(w stdhttp.ResponseWriter, credentials portyauth.Credentials, options RouterOptions) {
	secure := authCookieSecure(options)
	setCookie(w, sessionCookieName, credentials.AccessToken, "/", secure, int(portyauth.AccessLifetime/time.Second))
	setCookie(w, refreshCookieName, credentials.RefreshToken, "/api/v1/session", secure, int(portyauth.RefreshLifetime/time.Second))
	setCSRFCookie(w, credentials.CSRFToken, secure, int(portyauth.RefreshLifetime/time.Second))
}
func clearAuthCookies(w stdhttp.ResponseWriter, options RouterOptions) {
	secure := authCookieSecure(options)
	setCookie(w, sessionCookieName, "", "/", secure, -1)
	setCookie(w, refreshCookieName, "", "/api/v1/session", secure, -1)
	setCSRFCookie(w, "", secure, -1)
}

func writeAuthError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	switch {
	case errors.Is(err, portyauth.ErrAlreadyRegistered):
		WriteError(w, r, stdhttp.StatusConflict, "AlreadyRegistered", "Registration is closed", nil)
	case errors.Is(err, portyauth.ErrRateLimited):
		WriteError(w, r, stdhttp.StatusTooManyRequests, "RateLimited", "Too many authentication attempts", nil)
	case errors.Is(err, portyauth.ErrInvalidPassword), errors.Is(err, portyauth.ErrInvalidUsername):
		WriteError(w, r, stdhttp.StatusBadRequest, "InvalidCredentials", err.Error(), nil)
	case errors.Is(err, portyauth.ErrAuthenticationFailed):
		WriteError(w, r, stdhttp.StatusUnauthorized, "AuthenticationFailed", "Authentication failed", nil)
	default:
		WriteError(w, r, stdhttp.StatusInternalServerError, "InternalError", "Authentication could not be completed", nil)
	}
}

func decodeJSON(r *stdhttp.Request, value any) error {
	decoder := json.NewDecoder(stdhttp.MaxBytesReader(nil, r.Body, 1<<20))
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

func browserToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func setCookie(w stdhttp.ResponseWriter, name, value, path string, secure bool, maxAge int) {
	stdhttp.SetCookie(w, &stdhttp.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: secure, SameSite: stdhttp.SameSiteStrictMode, MaxAge: maxAge})
}

func setCSRFCookie(w stdhttp.ResponseWriter, value string, secure bool, maxAge int) {
	stdhttp.SetCookie(w, &stdhttp.Cookie{Name: csrfCookieName, Value: value, Path: "/", HttpOnly: false, Secure: secure, SameSite: stdhttp.SameSiteStrictMode, MaxAge: maxAge})
}
