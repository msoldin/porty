package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
	portyfs "github.com/msoldin/porty/internal/infrastructure/filesystem"
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
		mux.HandleFunc("POST /api/v1/repository/setup", mutationRoute(options, func(w http.ResponseWriter, r *http.Request) {
			var input domain.RepositorySetupRequest
			if decodeBody(w, r, &input) != nil {
				return
			}
			if err := options.RepositorySetup.SetupRepository(r.Context(), input); err != nil {
				writeAPIError(w, r, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]string{"status": "configured"})
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
			var value []domain.GitCommit
			var err error
			if paged, ok := options.Repository.(interface {
				RepositoryHistoryPage(context.Context, int, int) ([]domain.GitCommit, error)
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
			next(w, r)
		}
	}
}
func mutationRoute(options RouterOptions, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, session, ok := requireMutationAuth(w, r, options); ok {
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
