package http

import (
	"context"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
	stdhttp "net/http"
)

func registerRepositoryRoutes(mux *stdhttp.ServeMux, options RouterOptions) {
	if options.Repository != nil {
		mux.HandleFunc("GET /api/v1/repository/status", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			value, err := options.Repository.RepositoryStatus(r.Context())
			writeResult(w, r, value, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/repository/history", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var value []portyrepo.GitCommit
			var err error
			if paged, ok := options.Repository.(interface {
				RepositoryHistoryPage(context.Context, int, int) ([]portyrepo.GitCommit, error)
			}); ok {
				value, err = paged.RepositoryHistoryPage(r.Context(), queryLimit(r), queryOffset(r))
			} else {
				value, err = options.Repository.RepositoryHistory(r.Context(), queryLimit(r))
			}
			writeResult(w, r, value, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("GET /api/v1/stacks/{id}/diff", readRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			value, err := options.Repository.StackDiff(r.Context(), portystack.StackID(r.PathValue("id")))
			writeResult(w, r, map[string]string{"diff": value}, err, stdhttp.StatusOK)
		}))
		mux.HandleFunc("POST /api/v1/stacks/{id}/commit", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input struct {
				Message string `json:"message"`
			}
			if decodeBody(w, r, &input) != nil {
				return
			}
			sha, err := options.Repository.CommitStack(r.Context(), portystack.StackID(r.PathValue("id")), input.Message)
			writeResult(w, r, map[string]string{"sha": sha}, err, stdhttp.StatusCreated)
		}))
	}
	if options.Actions != nil {
		mux.HandleFunc("POST /api/v1/stacks/{id}/actions/{action}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			value, err := options.Actions.StartAction(r.Context(), portystack.StackID(r.PathValue("id")), r.PathValue("action"))
			writeResult(w, r, value, err, stdhttp.StatusAccepted)
		}))
		mux.HandleFunc("POST /api/v1/repository/actions/{action}", mutationRoute(options, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			value, err := options.Actions.StartRepositoryAction(r.Context(), r.PathValue("action"))
			writeResult(w, r, value, err, stdhttp.StatusAccepted)
		}))
	}
}
