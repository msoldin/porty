package http

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	portyauth "github.com/msoldin/porty/internal/auth"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyop "github.com/msoldin/porty/internal/operation"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
	"net"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	portyfs "github.com/msoldin/porty/internal/filesystem"
	"github.com/msoldin/porty/internal/http/middleware"
)

func registerAPIRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.Stacks != nil {
		mux.HandleFunc("GET /api/v1/stacks", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			items, err := options.Stacks.ListStacks(r.Context())
			writeResult(w, r, items, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/stacks", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input struct {
				Name string `json:"name"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			item, err := options.Stacks.CreateStack(r.Context(), input.Name)
			writeResult(w, r, item, err, stdhttp.StatusCreated)
		}))
		mux.HandleFunc("PATCH /api/v1/stacks/{id}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input struct {
				Name string `json:"name"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			item, err := options.Stacks.RenameStack(r.Context(), portystack.StackID(r.PathValue("id")), input.Name)
			writeResult(w, r, item, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("DELETE /api/v1/stacks/{id}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var err error
			if r.URL.Query().Get("purge") == "true" {
				err = options.Stacks.PurgeStack(r.Context(), portystack.StackID(r.PathValue("id")))
			} else {
				err = options.Stacks.DeleteStack(r.Context(), portystack.StackID(r.PathValue("id")))
			}
			if err != nil {
				writeAPIError(w, r, err)
				return
			}
			w.WriteHeader(stdhttp.StatusNoContent)
		}))
	}
	if options.Files != nil {
		mux.HandleFunc("GET /api/v1/stacks/{id}/tree", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			items, err := options.Files.Tree(r.Context(), portystack.StackID(r.PathValue("id")))
			writeResult(w, r, items, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/stacks/{id}/files", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			file, err := options.Files.ReadFile(r.Context(), portystack.StackID(r.PathValue("id")), r.URL.Query().Get("path"))
			if err != nil {
				writeAPIError(w, r, err)
				return
			}
			w.Header().Set("ETag", quoteETag(file.Hash))
			writeJSON(w, stdhttp.StatusOK, map[string]any{"path": file.Path, "content": string(file.Content), "hash": file.Hash, "size": file.Size})
		}))
		mux.HandleFunc("PUT /api/v1/stacks/{id}/files", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			expected := strings.Trim(r.Header.Get("If-Match"), `"`)
			if expected == "" {
				WriteError(w, r, stdhttp.StatusPreconditionRequired, "PreconditionRequired", "If-Match is required", nil)
				return
			}
			var input struct {
				Content string `json:"content"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			file, err := options.Files.WriteFile(r.Context(), portystack.StackID(r.PathValue("id")), r.URL.Query().Get("path"), []byte(input.Content), expected)
			if err != nil {
				writeAPIError(w, r, err)
				return
			}
			w.Header().Set("ETag", quoteETag(file.Hash))
			writeJSON(w, stdhttp.StatusOK, map[string]any{"path": file.Path, "hash": file.Hash, "size": file.Size})
		}))
		if mutations, ok := options.Files.(FileMutationAPI); ok {
			mux.HandleFunc("POST /api/v1/stacks/{id}/files", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				var input struct {
					Path      string `json:"path"`
					Content   string `json:"content"`
					Directory bool   `json:"directory"`
				}
				if decodeBody(w, r, &input) != nil {
					return
				}
				if input.Directory {
					if err := mutations.CreateDirectory(r.Context(), portystack.StackID(r.PathValue("id")), input.Path); err != nil {
						writeAPIError(w, r, err)
						return
					}
					writeJSON(w, stdhttp.StatusCreated, map[string]any{"path": input.Path, "directory": true})
					return
				}
				file, err := mutations.CreateFile(r.Context(), portystack.StackID(r.PathValue("id")), input.Path, []byte(input.Content))
				writeResult(w, r, file, err, stdhttp.StatusCreated)
			}))
			mux.HandleFunc("POST /api/v1/stacks/{id}/files/move", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				var input struct {
					From string `json:"from"`
					To   string `json:"to"`
				}
				if decodeBody(w, r, &input) != nil {
					return
				}
				if err := mutations.MoveFile(r.Context(), portystack.StackID(r.PathValue("id")), input.From, input.To); err != nil {
					writeAPIError(w, r, err)
					return
				}
				w.WriteHeader(stdhttp.StatusNoContent)
			}))
			mux.HandleFunc("DELETE /api/v1/stacks/{id}/files", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				if err := mutations.RemoveFile(r.Context(), portystack.StackID(r.PathValue("id")), r.URL.Query().Get("path")); err != nil {
					writeAPIError(w, r, err)
					return
				}
				w.WriteHeader(stdhttp.StatusNoContent)
			}))
		}
	}
	registerEnvironmentRoutes(mux, options)
	registerRepositoryRoutes(mux, options)
	registerOperationRoutes(mux, options)
	registerContainerRoutes(mux, options)
	if options.RepositorySetup != nil {
		mux.HandleFunc("GET /api/v1/repository/setup/status", setupReadRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			status, err := options.RepositorySetup.Status(r.Context())
			writeResult(w, r, status, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/repository/setup/inspect-remote", setupMutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ portyauth.Principal) {
			var input portyrepo.RemoteInspectionRequest
			if decodeBody(w, r, &input) != nil {
				return
			}
			inspection, err := options.RepositorySetup.InspectRemote(r.Context(), input)
			writeResult(w, r, inspection, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/repository/setup", setupMutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request, session portyauth.Principal) {
			var input portyrepo.RepositorySetupRequest
			if decodeBody(w, r, &input) != nil {
				return
			}
			status, err := options.RepositorySetup.Setup(r.Context(), input)
			recordRepositoryAudit(options, r, session.UserID, "repository.setup."+string(input.Mode), input.Branch, status, err)
			writeResult(w, r, status, err, stdhttp.StatusCreated)
		}))
		mux.HandleFunc("PUT /api/v1/repository/remote", setupMutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request, session portyauth.Principal) {
			if !requireRepositoryReady(w, r, options) {
				return
			}
			var input portyrepo.RepositoryRemoteRequest
			if decodeBody(w, r, &input) != nil {
				return
			}
			status, err := options.RepositorySetup.ConfigureRemote(r.Context(), input)
			recordRepositoryAudit(options, r, session.UserID, "repository.remote.configure", input.Branch, status, err)
			writeResult(w, r, status, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("DELETE /api/v1/repository/remote", setupMutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request, session portyauth.Principal) {
			if !requireRepositoryReady(w, r, options) {
				return
			}
			status, err := options.RepositorySetup.RemoveRemote(r.Context())
			recordRepositoryAudit(options, r, session.UserID, "repository.remote.remove", status.Branch, status, err)
			writeResult(w, r, status, err, stdhttp.StatusOK)
		}))
	}
	if options.State != nil {
		mux.HandleFunc("GET /api/v1/stacks/{id}/state", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			value, err := options.State.StackState(r.Context(), portystack.StackID(r.PathValue("id")))
			writeResult(w, r, value, err, stdhttp.StatusOK)
		}))
	}
	if options.Audit != nil {
		mux.HandleFunc("GET /api/v1/audit", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			value, err := options.Audit.AuditEvents(r.Context(), queryLimit(r), queryOffset(r))
			writeResult(w, r, value, err, stdhttp.StatusOK)
		}))
	}
	if options.Stream != nil {
		mux.HandleFunc("GET /api/v1/stream", authenticatedRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if !requireRepositoryReady(w, r, options) {
				return
			}
			ctx, cancel := context.WithDeadline(r.Context(), principalFrom(r.Context()).ExpiresAt)
			defer cancel()
			options.Stream.ServeHTTP(w, r.WithContext(ctx))
		}))
	}
}

func readRoute(options RouterOptions, next stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	return authenticatedRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !requireRepositoryReady(w, r, options) {
			return
		}
		next(w, r)
	})
}
func mutationRoute(options RouterOptions, next stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	return mutationAuthRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !requireRepositoryReady(w, r, options) {
			return
		}
		recorder := &statusWriter{ResponseWriter: w, status: stdhttp.StatusOK}
		next(recorder, r)
		outcome := "succeeded"
		if recorder.status >= 400 {
			outcome = "failed"
		}
		recordAudit(options, r, principalFrom(r.Context()).UserID, r.Method+" "+r.Pattern, "http", r.PathValue("id"), outcome)
	})
}

func setupReadRoute(options RouterOptions, next stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	return authenticatedRoute(options, next)
}

func setupMutationRoute(options RouterOptions, next func(stdhttp.ResponseWriter, *stdhttp.Request, portyauth.Principal)) stdhttp.HandlerFunc {
	return mutationAuthRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		next(w, r, principalFrom(r.Context()))
	})
}

func requireRepositoryReady(w stdhttp.ResponseWriter, r *stdhttp.Request, options RouterOptions) bool {
	if options.RepositorySetup == nil {
		return true
	}
	ready, err := options.RepositorySetup.Ready(r.Context())
	if err != nil {
		writeAPIError(w, r, err)
		return false
	}
	if !ready {
		WriteError(w, r, stdhttp.StatusConflict, "RepositorySetupRequired", "Configure the stack repository before using Porty.", nil)
		return false
	}
	return true
}

func recordRepositoryAudit(options RouterOptions, r *stdhttp.Request, actor, action, branch string, status portyrepo.RepositorySetupStatus, err error) {
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
	stdhttp.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != stdhttp.StatusOK {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func recordAudit(options RouterOptions, r *stdhttp.Request, actor, action, targetType, targetID, outcome string) {
	if options.Audit == nil {
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	var random [12]byte
	_, _ = rand.Read(random[:])
	_ = options.Audit.RecordAudit(r.Context(), portycontrol.AuditEvent{ID: "aud_" + hex.EncodeToString(random[:]), ActorUserID: actor, Action: action, TargetType: targetType, TargetID: targetID, Outcome: outcome, RequestID: middleware.RequestID(r.Context()), SourceIP: host, OccurredAt: time.Now().UTC()})
}
func (w *statusWriter) Write(value []byte) (int, error) { return w.ResponseWriter.Write(value) }

func decodeBody(w stdhttp.ResponseWriter, r *stdhttp.Request, value any) error {
	if err := decodeJSON(r, value); err != nil {
		var tooLarge *stdhttp.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteError(w, r, stdhttp.StatusRequestEntityTooLarge, "LimitExceeded", "The request body exceeds a limit", nil)
			return err
		}
		WriteError(w, r, stdhttp.StatusBadRequest, "InvalidRequest", "Request body is invalid", nil)
		return err
	}
	return nil
}

func writeResult(w stdhttp.ResponseWriter, r *stdhttp.Request, value any, err error, status int) {
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, status, value)
}

func writeAPIError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	switch {
	case errors.Is(err, portycontrol.ErrContainerNotFound):
		WriteError(w, r, stdhttp.StatusNotFound, "ContainerNotFound", "Container not found in this stack", nil)
	case errors.Is(err, portycontrol.ErrUnsupportedContainerAction):
		WriteError(w, r, stdhttp.StatusBadRequest, "InvalidContainerAction", "Container action is invalid", nil)
	case errors.Is(err, portycontrol.ErrContainerStateConflict), errors.Is(err, portycontrol.ErrContainerArchived):
		WriteError(w, r, stdhttp.StatusConflict, "ContainerStateConflict", "Container action is unavailable", nil)
	case errors.Is(err, sql.ErrNoRows):
		WriteError(w, r, stdhttp.StatusNotFound, "NotFound", "Resource not found", nil)
	case errors.Is(err, portyfs.ErrInvalidPath), errors.Is(err, portystack.ErrInvalidEnvironment):
		WriteError(w, r, stdhttp.StatusBadRequest, "InvalidRequest", "The request is invalid", nil)
	case errors.Is(err, portystack.ErrEnvironmentSecretImmutable):
		WriteError(w, r, stdhttp.StatusConflict, "EnvironmentSecretImmutable", "Delete and recreate the value to change its secret setting", nil)
	case errors.Is(err, portyfs.ErrStaleFile):
		WriteError(w, r, stdhttp.StatusPreconditionFailed, "StaleFile", "The file changed since it was opened", nil)
	case errors.Is(err, portyfs.ErrTooLarge):
		WriteError(w, r, stdhttp.StatusRequestEntityTooLarge, "LimitExceeded", "The requested content exceeds a limit", nil)
	case errors.Is(err, portyop.ErrOperationConflict):
		WriteError(w, r, stdhttp.StatusConflict, "OperationConflict", "A conflicting operation is in progress", nil)
	case errors.Is(err, portyrepo.ErrInvalidRequest):
		WriteError(w, r, stdhttp.StatusBadRequest, "InvalidRequest", "The request is invalid", nil)
	case errors.Is(err, portyrepo.ErrRepositorySetupRequired):
		WriteError(w, r, stdhttp.StatusConflict, "RepositorySetupRequired", "Configure the stack repository before using Porty.", nil)
	case errors.Is(err, portyrepo.ErrRepositoryPathNotEmpty):
		WriteError(w, r, stdhttp.StatusConflict, "RepositoryPathNotEmpty", "The repository path is not empty", nil)
	case errors.Is(err, portyrepo.ErrInvalidWorktree):
		WriteError(w, r, stdhttp.StatusConflict, "InvalidWorktree", "The repository worktree is invalid", nil)
	case errors.Is(err, portyrepo.ErrDetachedHead):
		WriteError(w, r, stdhttp.StatusConflict, "DetachedHead", "Check out a branch before adopting the repository", nil)
	case errors.Is(err, portyrepo.ErrRemoteAuthenticationFailed):
		WriteError(w, r, stdhttp.StatusUnauthorized, "RemoteAuthenticationFailed", "Remote authentication failed", nil)
	case errors.Is(err, portyrepo.ErrRemoteUnavailable):
		WriteError(w, r, stdhttp.StatusBadGateway, "RemoteUnavailable", "The remote repository is unavailable", nil)
	case errors.Is(err, portyrepo.ErrSSHMaterialUnavailable):
		WriteError(w, r, stdhttp.StatusConflict, "SSHMaterialUnavailable", "SSH identity files are unavailable or unsafe", nil)
	case errors.Is(err, portyrepo.ErrUnrelatedHistory):
		WriteError(w, r, stdhttp.StatusConflict, "UnrelatedHistory", "Local and remote repository histories are unrelated", nil)
	case errors.Is(err, portyrepo.ErrRepositoryRemoteConflict):
		WriteError(w, r, stdhttp.StatusConflict, "RepositoryRemoteConflict", "The existing origin requires explicit replacement", nil)
	case errors.Is(err, portyrepo.ErrRepositoryRemoteUnavailable):
		WriteError(w, r, stdhttp.StatusConflict, "RepositoryRemoteUnavailable", "No managed remote is configured", nil)
	default:
		WriteError(w, r, stdhttp.StatusInternalServerError, "InternalError", "The request could not be completed", nil)
	}
}

func quoteETag(value string) string { return `"` + strings.Trim(value, `"`) + `"` }
func queryLimit(r *stdhttp.Request) int {
	value, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return value
}
func queryOffset(r *stdhttp.Request) int {
	value, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if value < 0 {
		return 0
	}
	return value
}
