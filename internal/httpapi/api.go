package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
	portyfs "github.com/msoldin/porty/internal/filesystem"
)

func registerAPIRoutes(mux *http.ServeMux, options RouterOptions) {
	if options.Stacks != nil {
		mux.HandleFunc("GET /api/v1/stacks", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			items, err := options.Stacks.ListStacks(r.Context())
			writeResult(w, r, items, err, http.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/stacks", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Name string `json:"name"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			item, err := options.Stacks.CreateStack(r.Context(), input.Name)
			writeResult(w, r, item, err, http.StatusCreated)
		}))
		mux.HandleFunc("PATCH /api/v1/stacks/{id}", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Name string `json:"name"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			item, err := options.Stacks.RenameStack(r.Context(), domain.StackID(r.PathValue("id")), input.Name)
			writeResult(w, r, item, err, http.StatusOK)
		}))
		mux.HandleFunc("DELETE /api/v1/stacks/{id}", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			var err error
			if r.URL.Query().Get("purge") == "true" {
				err = options.Stacks.PurgeStack(r.Context(), domain.StackID(r.PathValue("id")))
			} else {
				err = options.Stacks.DeleteStack(r.Context(), domain.StackID(r.PathValue("id")))
			}
			if err != nil {
				writeAPIError(w, r, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
	}
	if options.Files != nil {
		mux.HandleFunc("GET /api/v1/stacks/{id}/tree", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			items, err := options.Files.Tree(r.Context(), domain.StackID(r.PathValue("id")))
			writeResult(w, r, items, err, http.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/stacks/{id}/files", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			file, err := options.Files.ReadFile(r.Context(), domain.StackID(r.PathValue("id")), r.URL.Query().Get("path"))
			if err != nil {
				writeAPIError(w, r, err)
				return
			}
			w.Header().Set("ETag", quoteETag(file.Hash))
			writeJSON(w, http.StatusOK, map[string]any{"path": file.Path, "content": string(file.Content), "hash": file.Hash, "size": file.Size})
		}))
		mux.HandleFunc("PUT /api/v1/stacks/{id}/files", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			expected := strings.Trim(r.Header.Get("If-Match"), `"`)
			if expected == "" {
				WriteError(w, r, http.StatusPreconditionRequired, "PreconditionRequired", "If-Match is required", nil)
				return
			}
			var input struct {
				Content string `json:"content"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			file, err := options.Files.WriteFile(r.Context(), domain.StackID(r.PathValue("id")), r.URL.Query().Get("path"), []byte(input.Content), expected)
			if err != nil {
				writeAPIError(w, r, err)
				return
			}
			w.Header().Set("ETag", quoteETag(file.Hash))
			writeJSON(w, http.StatusOK, map[string]any{"path": file.Path, "hash": file.Hash, "size": file.Size})
		}))
		if mutations, ok := options.Files.(FileMutationAPI); ok {
			mux.HandleFunc("POST /api/v1/stacks/{id}/files", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Path      string `json:"path"`
					Content   string `json:"content"`
					Directory bool   `json:"directory"`
				}
				if decodeBody(w, r, &input) != nil {
					return
				}
				if input.Directory {
					if err := mutations.CreateDirectory(r.Context(), domain.StackID(r.PathValue("id")), input.Path); err != nil {
						writeAPIError(w, r, err)
						return
					}
					writeJSON(w, http.StatusCreated, map[string]any{"path": input.Path, "directory": true})
					return
				}
				file, err := mutations.CreateFile(r.Context(), domain.StackID(r.PathValue("id")), input.Path, []byte(input.Content))
				writeResult(w, r, file, err, http.StatusCreated)
			}))
			mux.HandleFunc("POST /api/v1/stacks/{id}/files/move", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					From string `json:"from"`
					To   string `json:"to"`
				}
				if decodeBody(w, r, &input) != nil {
					return
				}
				if err := mutations.MoveFile(r.Context(), domain.StackID(r.PathValue("id")), input.From, input.To); err != nil {
					writeAPIError(w, r, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			mux.HandleFunc("DELETE /api/v1/stacks/{id}/files", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
				if err := mutations.RemoveFile(r.Context(), domain.StackID(r.PathValue("id")), r.URL.Query().Get("path")); err != nil {
					writeAPIError(w, r, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
		}
	}
	registerEnvironmentRoutes(mux, options)
	registerRepositoryRoutes(mux, options)
	registerOperationRoutes(mux, options)
	if options.RepositorySetup != nil {
		mux.HandleFunc("GET /api/v1/repository/setup/status", setupReadRoute(options, func(w http.ResponseWriter, r *http.Request) {
			status, err := options.RepositorySetup.Status(r.Context())
			writeResult(w, r, status, err, http.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/repository/setup/inspect-remote", setupMutationRoute(options, func(w http.ResponseWriter, r *http.Request, _ application.AuthenticatedSession) {
			var input portyrepo.RemoteInspectionRequest
			if decodeBody(w, r, &input) != nil {
				return
			}
			inspection, err := options.RepositorySetup.InspectRemote(r.Context(), input)
			writeResult(w, r, inspection, err, http.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/repository/setup", setupMutationRoute(options, func(w http.ResponseWriter, r *http.Request, session application.AuthenticatedSession) {
			var input portyrepo.RepositorySetupRequest
			if decodeBody(w, r, &input) != nil {
				return
			}
			status, err := options.RepositorySetup.Setup(r.Context(), input)
			recordRepositoryAudit(options, r, session.UserID, "repository.setup."+string(input.Mode), input.Branch, status, err)
			writeResult(w, r, status, err, http.StatusCreated)
		}))
		mux.HandleFunc("PUT /api/v1/repository/remote", setupMutationRoute(options, func(w http.ResponseWriter, r *http.Request, session application.AuthenticatedSession) {
			if !requireRepositoryReady(w, r, options) {
				return
			}
			var input portyrepo.RepositoryRemoteRequest
			if decodeBody(w, r, &input) != nil {
				return
			}
			status, err := options.RepositorySetup.ConfigureRemote(r.Context(), input)
			recordRepositoryAudit(options, r, session.UserID, "repository.remote.configure", input.Branch, status, err)
			writeResult(w, r, status, err, http.StatusOK)
		}))
		mux.HandleFunc("DELETE /api/v1/repository/remote", setupMutationRoute(options, func(w http.ResponseWriter, r *http.Request, session application.AuthenticatedSession) {
			if !requireRepositoryReady(w, r, options) {
				return
			}
			status, err := options.RepositorySetup.RemoveRemote(r.Context())
			recordRepositoryAudit(options, r, session.UserID, "repository.remote.remove", status.Branch, status, err)
			writeResult(w, r, status, err, http.StatusOK)
		}))
	}
	if options.State != nil {
		mux.HandleFunc("GET /api/v1/stacks/{id}/state", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			value, err := options.State.StackState(r.Context(), domain.StackID(r.PathValue("id")))
			writeResult(w, r, value, err, http.StatusOK)
		}))
	}
	if options.Audit != nil {
		mux.HandleFunc("GET /api/v1/audit", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			value, err := options.Audit.AuditEvents(r.Context(), queryLimit(r), queryOffset(r))
			writeResult(w, r, value, err, http.StatusOK)
		}))
	}
	if options.Stream != nil {
		mux.HandleFunc("GET /api/v1/stream", func(w http.ResponseWriter, r *http.Request) {
			if _, _, ok := authenticate(w, r, options.Auth); ok {
				if !requireRepositoryReady(w, r, options) {
					return
				}
				options.Stream.ServeHTTP(w, r)
			}
		})
	}
}

func registerEnvironmentRoutes(mux *http.ServeMux, options RouterOptions) {
	if options.Environment == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/stacks/{id}/environment", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
		keys, err := options.Environment.EnvironmentKeys(r.Context(), domain.StackID(r.PathValue("id")))
		writeResult(w, r, map[string]any{"keys": keys}, err, http.StatusOK)
	}))
	mux.HandleFunc("PUT /api/v1/stacks/{id}/environment/{key}", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Value string `json:"value"`
		}
		if decodeBody(w, r, &input) != nil {
			return
		}
		if err := options.Environment.SetEnvironment(r.Context(), domain.StackID(r.PathValue("id")), r.PathValue("key"), input.Value); err != nil {
			writeAPIError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("DELETE /api/v1/stacks/{id}/environment/{key}", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
		if err := options.Environment.DeleteEnvironment(r.Context(), domain.StackID(r.PathValue("id")), r.PathValue("key")); err != nil {
			writeAPIError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func registerRepositoryRoutes(mux *http.ServeMux, options RouterOptions) {
	if options.Repository != nil {
		mux.HandleFunc("GET /api/v1/repository/status", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			value, err := options.Repository.RepositoryStatus(r.Context())
			writeResult(w, r, value, err, http.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/repository/history", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			var value []portyrepo.GitCommit
			var err error
			if paged, ok := options.Repository.(interface {
				RepositoryHistoryPage(context.Context, int, int) ([]portyrepo.GitCommit, error)
			}); ok {
				value, err = paged.RepositoryHistoryPage(r.Context(), queryLimit(r), queryOffset(r))
			} else {
				value, err = options.Repository.RepositoryHistory(r.Context(), queryLimit(r))
			}
			writeResult(w, r, value, err, http.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/stacks/{id}/diff", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			value, err := options.Repository.StackDiff(r.Context(), domain.StackID(r.PathValue("id")))
			writeResult(w, r, map[string]string{"diff": value}, err, http.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/stacks/{id}/commit", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Message string `json:"message"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			sha, err := options.Repository.CommitStack(r.Context(), domain.StackID(r.PathValue("id")), input.Message)
			writeResult(w, r, map[string]string{"sha": sha}, err, http.StatusCreated)
		}))
	}
	if options.Actions != nil {
		mux.HandleFunc("POST /api/v1/stacks/{id}/actions/{action}", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			value, err := options.Actions.StartAction(r.Context(), domain.StackID(r.PathValue("id")), r.PathValue("action"))
			writeResult(w, r, value, err, http.StatusAccepted)
		}))
		mux.HandleFunc("POST /api/v1/repository/actions/{action}", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			value, err := options.Actions.StartRepositoryAction(r.Context(), r.PathValue("action"))
			writeResult(w, r, value, err, http.StatusAccepted)
		}))
	}
}

func registerOperationRoutes(mux *http.ServeMux, options RouterOptions) {
	if options.Operations != nil {
		mux.HandleFunc("GET /api/v1/operations", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			var value []domain.Operation
			var err error
			if paged, ok := options.Operations.(interface {
				OperationsPage(context.Context, int, int) ([]domain.Operation, error)
			}); ok {
				value, err = paged.OperationsPage(r.Context(), queryLimit(r), queryOffset(r))
			} else {
				value, err = options.Operations.Operations(r.Context(), queryLimit(r))
			}
			writeResult(w, r, value, err, http.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/operations/{id}", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			value, err := options.Operations.Operation(r.Context(), r.PathValue("id"))
			writeResult(w, r, value, err, http.StatusOK)
		}))
	}
	if options.Deployments != nil {
		mux.HandleFunc("GET /api/v1/stacks/{id}/deployments", readRoute(options, func(w http.ResponseWriter, r *http.Request) {
			id := domain.StackID(r.PathValue("id"))
			var value []domain.Deployment
			var err error
			if paged, ok := options.Deployments.(interface {
				DeploymentsPage(context.Context, domain.StackID, int, int) ([]domain.Deployment, error)
			}); ok {
				value, err = paged.DeploymentsPage(r.Context(), id, queryLimit(r), queryOffset(r))
			} else {
				value, err = options.Deployments.Deployments(r.Context(), id, queryLimit(r))
			}
			writeResult(w, r, value, err, http.StatusOK)
		}))
	}
}

func readRoute(options RouterOptions, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := authenticate(w, r, options.Auth); ok {
			if !requireRepositoryReady(w, r, options) {
				return
			}
			next(w, r)
		}
	}
}
func mutationRoute(options RouterOptions, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, session, ok := requireMutationAuth(w, r, options); ok {
			if !requireRepositoryReady(w, r, options) {
				return
			}
			recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next(recorder, r)
			outcome := "succeeded"
			if recorder.status >= 400 {
				outcome = "failed"
			}
			recordAudit(options, r, session.UserID, r.Method+" "+r.Pattern, "http", r.PathValue("id"), outcome)
		}
	}
}

func setupReadRoute(options RouterOptions, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := authenticate(w, r, options.Auth); ok {
			next(w, r)
		}
	}
}

func setupMutationRoute(options RouterOptions, next func(http.ResponseWriter, *http.Request, application.AuthenticatedSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, session, ok := requireMutationAuth(w, r, options); ok {
			next(w, r, session)
		}
	}
}

func requireRepositoryReady(w http.ResponseWriter, r *http.Request, options RouterOptions) bool {
	if options.RepositorySetup == nil {
		return true
	}
	ready, err := options.RepositorySetup.Ready(r.Context())
	if err != nil {
		writeAPIError(w, r, err)
		return false
	}
	if !ready {
		WriteError(w, r, http.StatusConflict, "RepositorySetupRequired", "Configure the stack repository before using Porty.", nil)
		return false
	}
	return true
}

func recordRepositoryAudit(options RouterOptions, r *http.Request, actor, action, branch string, status portyrepo.RepositorySetupStatus, err error) {
	outcome := "succeeded"
	targetID := "branch=" + branch
	if err != nil {
		outcome = "failed"
		targetID += ";code=" + repositoryErrorCode(err)
	} else if status.ManagedRemote != nil {
		targetID += ";remote=" + status.ManagedRemote.URL
	}
	recordAudit(options, r, actor, action, "repository", targetID, outcome)
}

func repositoryErrorCode(err error) string {
	switch {
	case errors.Is(err, portyrepo.ErrInvalidRequest):
		return "InvalidRequest"
	case errors.Is(err, portyrepo.ErrRepositorySetupRequired):
		return "RepositorySetupRequired"
	case errors.Is(err, portyrepo.ErrRepositoryPathNotEmpty):
		return "RepositoryPathNotEmpty"
	case errors.Is(err, portyrepo.ErrInvalidWorktree):
		return "InvalidWorktree"
	case errors.Is(err, portyrepo.ErrDetachedHead):
		return "DetachedHead"
	case errors.Is(err, portyrepo.ErrRemoteAuthenticationFailed):
		return "RemoteAuthenticationFailed"
	case errors.Is(err, portyrepo.ErrRemoteUnavailable):
		return "RemoteUnavailable"
	case errors.Is(err, portyrepo.ErrSSHMaterialUnavailable):
		return "SSHMaterialUnavailable"
	case errors.Is(err, portyrepo.ErrUnrelatedHistory):
		return "UnrelatedHistory"
	case errors.Is(err, portyrepo.ErrRepositoryRemoteConflict):
		return "RepositoryRemoteConflict"
	case errors.Is(err, portyrepo.ErrRepositoryRemoteUnavailable):
		return "RepositoryRemoteUnavailable"
	default:
		return "InternalError"
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != http.StatusOK {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func recordAudit(options RouterOptions, r *http.Request, actor, action, targetType, targetID, outcome string) {
	if options.Audit == nil {
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	var random [12]byte
	_, _ = rand.Read(random[:])
	_ = options.Audit.RecordAudit(r.Context(), domain.AuditEvent{ID: "aud_" + hex.EncodeToString(random[:]), ActorUserID: actor, Action: action, TargetType: targetType, TargetID: targetID, Outcome: outcome, RequestID: RequestID(r.Context()), SourceIP: host, OccurredAt: time.Now().UTC()})
}
func (w *statusWriter) Write(value []byte) (int, error) { return w.ResponseWriter.Write(value) }

func decodeBody(w http.ResponseWriter, r *http.Request, value any) error {
	if err := decodeJSON(r, value); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteError(w, r, http.StatusRequestEntityTooLarge, "LimitExceeded", "The request body exceeds a limit", nil)
			return err
		}
		WriteError(w, r, http.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
		return err
	}
	return nil
}

func writeResult(w http.ResponseWriter, r *http.Request, value any, err error, status int) {
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, status, value)
}

func writeAPIError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		WriteError(w, r, http.StatusNotFound, "NotFound", "Resource not found", nil)
	case errors.Is(err, portyfs.ErrInvalidPath), errors.Is(err, application.ErrInvalidEnvironment):
		WriteError(w, r, http.StatusBadRequest, "InvalidRequest", "The request is invalid", nil)
	case errors.Is(err, portyfs.ErrStaleFile):
		WriteError(w, r, http.StatusPreconditionFailed, "StaleFile", "The file changed since it was opened", nil)
	case errors.Is(err, portyfs.ErrTooLarge):
		WriteError(w, r, http.StatusRequestEntityTooLarge, "LimitExceeded", "The requested content exceeds a limit", nil)
	case errors.Is(err, application.ErrOperationConflict):
		WriteError(w, r, http.StatusConflict, "OperationConflict", "A conflicting operation is in progress", nil)
	case errors.Is(err, portyrepo.ErrInvalidRequest):
		WriteError(w, r, http.StatusBadRequest, "InvalidRequest", "The request is invalid", nil)
	case errors.Is(err, portyrepo.ErrRepositorySetupRequired):
		WriteError(w, r, http.StatusConflict, "RepositorySetupRequired", "Configure the stack repository before using Porty.", nil)
	case errors.Is(err, portyrepo.ErrRepositoryPathNotEmpty):
		WriteError(w, r, http.StatusConflict, "RepositoryPathNotEmpty", "The repository path is not empty", nil)
	case errors.Is(err, portyrepo.ErrInvalidWorktree):
		WriteError(w, r, http.StatusConflict, "InvalidWorktree", "The repository worktree is invalid", nil)
	case errors.Is(err, portyrepo.ErrDetachedHead):
		WriteError(w, r, http.StatusConflict, "DetachedHead", "Check out a branch before adopting the repository", nil)
	case errors.Is(err, portyrepo.ErrRemoteAuthenticationFailed):
		WriteError(w, r, http.StatusUnauthorized, "RemoteAuthenticationFailed", "Remote authentication failed", nil)
	case errors.Is(err, portyrepo.ErrRemoteUnavailable):
		WriteError(w, r, http.StatusBadGateway, "RemoteUnavailable", "The remote repository is unavailable", nil)
	case errors.Is(err, portyrepo.ErrSSHMaterialUnavailable):
		WriteError(w, r, http.StatusConflict, "SSHMaterialUnavailable", "SSH identity files are unavailable or unsafe", nil)
	case errors.Is(err, portyrepo.ErrUnrelatedHistory):
		WriteError(w, r, http.StatusConflict, "UnrelatedHistory", "Local and remote repository histories are unrelated", nil)
	case errors.Is(err, portyrepo.ErrRepositoryRemoteConflict):
		WriteError(w, r, http.StatusConflict, "RepositoryRemoteConflict", "The existing origin requires explicit replacement", nil)
	case errors.Is(err, portyrepo.ErrRepositoryRemoteUnavailable):
		WriteError(w, r, http.StatusConflict, "RepositoryRemoteUnavailable", "No managed remote is configured", nil)
	default:
		WriteError(w, r, http.StatusInternalServerError, "InternalError", "The request could not be completed", nil)
	}
}

func quoteETag(value string) string { return `"` + strings.Trim(value, `"`) + `"` }
func queryLimit(r *http.Request) int {
	value, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return value
}
func queryOffset(r *http.Request) int {
	value, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if value < 0 {
		return 0
	}
	return value
}
